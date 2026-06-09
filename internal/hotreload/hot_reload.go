package hotreload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"reflect"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"tribeway/internal/logger"
)

// HotReloadManager 热更新管理器
type HotReloadManager struct {
	watcher   *fsnotify.Watcher
	modules   map[string]*Module
	configs   map[string]*ConfigFile
	mutex     sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	callbacks map[string][]ReloadCallback
}

// Module 可热更新的模块
type Module struct {
	Name         string
	Path         string
	Plugin       *plugin.Plugin
	LastModTime  time.Time
	Version      string
	Dependencies []string
}

// ConfigFile 可热更新的配置文件
type ConfigFile struct {
	Path        string
	LastModTime time.Time
	Parser      ConfigParser
	Data        interface{}
}

// ReloadCallback 重新加载回调函数
type ReloadCallback func(name string, oldData, newData interface{}) error

// ConfigParser 配置解析器接口
type ConfigParser interface {
	Parse(data []byte) (interface{}, error)
	Validate(data interface{}) error
}

// NewHotReloadManager 创建热更新管理器
func NewHotReloadManager() (*HotReloadManager, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	manager := &HotReloadManager{
		watcher:   watcher,
		modules:   make(map[string]*Module),
		configs:   make(map[string]*ConfigFile),
		ctx:       ctx,
		cancel:    cancel,
		callbacks: make(map[string][]ReloadCallback),
	}

	go manager.watchLoop()

	logger.Info("Hot reload manager initialized")
	return manager, nil
}

// RegisterModule 注册可热更新模块
func (hrm *HotReloadManager) RegisterModule(name, path string, dependencies []string) error {
	hrm.mutex.Lock()
	defer hrm.mutex.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %v", err)
	}

	// 获取文件修改时间
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %v", err)
	}

	module := &Module{
		Name:         name,
		Path:         absPath,
		LastModTime:  fileInfo.ModTime(),
		Dependencies: dependencies,
	}

	// 初始加载模块
	if err := hrm.loadModule(module); err != nil {
		return fmt.Errorf("failed to load module: %v", err)
	}

	hrm.modules[name] = module

	// 监控文件变化
	if err := hrm.watcher.Add(absPath); err != nil {
		return fmt.Errorf("failed to add file to watcher: %v", err)
	}

	logger.Info(fmt.Sprintf("Registered hot reload module: %s", name))
	return nil
}

// RegisterConfig 注册可热更新配置文件
func (hrm *HotReloadManager) RegisterConfig(path string, parser ConfigParser) error {
	hrm.mutex.Lock()
	defer hrm.mutex.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %v", err)
	}

	// 获取文件修改时间
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %v", err)
	}

	config := &ConfigFile{
		Path:        absPath,
		LastModTime: fileInfo.ModTime(),
		Parser:      parser,
	}

	// 初始加载配置
	if err := hrm.loadConfig(config); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	hrm.configs[absPath] = config

	// 监控文件变化
	if err := hrm.watcher.Add(absPath); err != nil {
		return fmt.Errorf("failed to add file to watcher: %v", err)
	}

	logger.Info(fmt.Sprintf("Registered hot reload config: %s", path))
	return nil
}

// RegisterCallback 注册重新加载回调
func (hrm *HotReloadManager) RegisterCallback(name string, callback ReloadCallback) {
	hrm.mutex.Lock()
	defer hrm.mutex.Unlock()

	hrm.callbacks[name] = append(hrm.callbacks[name], callback)
}

// loadModule 加载模块
func (hrm *HotReloadManager) loadModule(module *Module) error {
	// 构建Go插件
	if err := hrm.buildPlugin(module); err != nil {
		return fmt.Errorf("failed to build plugin: %v", err)
	}

	// 加载插件
	plug, err := plugin.Open(module.Path)
	if err != nil {
		return fmt.Errorf("failed to open plugin: %v", err)
	}

	module.Plugin = plug
	module.Version = fmt.Sprintf("%d", time.Now().Unix())

	logger.Info(fmt.Sprintf("Loaded module: %s (version: %s)", module.Name, module.Version))
	return nil
}

// loadConfig 加载配置文件
func (hrm *HotReloadManager) loadConfig(config *ConfigFile) error {
	data, err := os.ReadFile(config.Path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	// 解析配置
	parsedData, err := config.Parser.Parse(data)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}

	// 验证配置
	if err := config.Parser.Validate(parsedData); err != nil {
		return fmt.Errorf("config validation failed: %v", err)
	}

	oldData := config.Data
	config.Data = parsedData

	// 执行回调
	callbacks := hrm.callbacks[config.Path]
	for _, callback := range callbacks {
		if err := callback(config.Path, oldData, parsedData); err != nil {
			logger.Error(fmt.Sprintf("Config reload callback failed: %v", err))
		}
	}

	logger.Info(fmt.Sprintf("Loaded config: %s", config.Path))
	return nil
}

// buildPlugin 构建Go插件
func (hrm *HotReloadManager) buildPlugin(module *Module) error {
	// 这里应该实现Go插件的构建逻辑
	// 例如调用 go build -buildmode=plugin
	logger.Debug(fmt.Sprintf("Building plugin for module: %s", module.Name))
	return nil
}

// watchLoop 监控文件变化循环
func (hrm *HotReloadManager) watchLoop() {
	for {
		select {
		case event, ok := <-hrm.watcher.Events:
			if !ok {
				return
			}

			if event.Op&fsnotify.Write == fsnotify.Write {
				hrm.handleFileChange(event.Name)
			}

		case err, ok := <-hrm.watcher.Errors:
			if !ok {
				return
			}
			logger.Error(fmt.Sprintf("File watcher error: %v", err))

		case <-hrm.ctx.Done():
			return
		}
	}
}

// handleFileChange 处理文件变化
func (hrm *HotReloadManager) handleFileChange(path string) {
	hrm.mutex.Lock()
	defer hrm.mutex.Unlock()

	// 防抖动：短时间内的多次修改只处理一次
	time.Sleep(100 * time.Millisecond)

	absPath, err := filepath.Abs(path)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to get absolute path: %v", err))
		return
	}

	// 检查是否是注册的模块
	for _, module := range hrm.modules {
		if module.Path == absPath {
			hrm.reloadModule(module)
			return
		}
	}

	// 检查是否是注册的配置文件
	if config, exists := hrm.configs[absPath]; exists {
		hrm.reloadConfig(config)
	}
}

// reloadModule 重新加载模块
func (hrm *HotReloadManager) reloadModule(module *Module) {
	logger.Info(fmt.Sprintf("Reloading module: %s", module.Name))

	// 获取文件修改时间
	fileInfo, err := os.Stat(module.Path)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to stat module file: %v", err))
		return
	}

	// 检查是否真的有修改
	if !fileInfo.ModTime().After(module.LastModTime) {
		return
	}

	module.LastModTime = fileInfo.ModTime()

	// 重新加载模块
	if err := hrm.loadModule(module); err != nil {
		logger.Error(fmt.Sprintf("Failed to reload module: %v", err))
		return
	}

	// 执行回调
	callbacks := hrm.callbacks[module.Name]
	for _, callback := range callbacks {
		if err := callback(module.Name, nil, module); err != nil {
			logger.Error(fmt.Sprintf("Module reload callback failed: %v", err))
		}
	}

	logger.Info(fmt.Sprintf("Module reloaded successfully: %s", module.Name))
}

// reloadConfig 重新加载配置文件
func (hrm *HotReloadManager) reloadConfig(config *ConfigFile) {
	logger.Info(fmt.Sprintf("Reloading config: %s", config.Path))

	// 获取文件修改时间
	fileInfo, err := os.Stat(config.Path)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to stat config file: %v", err))
		return
	}

	// 检查是否真的有修改
	if !fileInfo.ModTime().After(config.LastModTime) {
		return
	}

	config.LastModTime = fileInfo.ModTime()

	// 重新加载配置
	if err := hrm.loadConfig(config); err != nil {
		logger.Error(fmt.Sprintf("Failed to reload config: %v", err))
	}
}

// GetModule 获取模块
func (hrm *HotReloadManager) GetModule(name string) (*Module, bool) {
	hrm.mutex.RLock()
	defer hrm.mutex.RUnlock()

	module, exists := hrm.modules[name]
	return module, exists
}

// GetConfig 获取配置
func (hrm *HotReloadManager) GetConfig(path string) (interface{}, bool) {
	hrm.mutex.RLock()
	defer hrm.mutex.RUnlock()

	config, exists := hrm.configs[path]
	if !exists {
		return nil, false
	}
	return config.Data, true
}

// InvokeModuleFunction 调用模块函数
func (hrm *HotReloadManager) InvokeModuleFunction(moduleName, functionName string, args ...interface{}) ([]reflect.Value, error) {
	module, exists := hrm.GetModule(moduleName)
	if !exists {
		return nil, fmt.Errorf("module not found: %s", moduleName)
	}

	if module.Plugin == nil {
		return nil, fmt.Errorf("module plugin not loaded: %s", moduleName)
	}

	// 获取函数符号
	symbol, err := module.Plugin.Lookup(functionName)
	if err != nil {
		return nil, fmt.Errorf("function not found: %s.%s", moduleName, functionName)
	}

	// 转换为函数
	fn := reflect.ValueOf(symbol)
	if fn.Kind() != reflect.Func {
		return nil, fmt.Errorf("symbol is not a function: %s.%s", moduleName, functionName)
	}

	// 准备参数
	values := make([]reflect.Value, len(args))
	for i, arg := range args {
		values[i] = reflect.ValueOf(arg)
	}

	// 调用函数
	result := fn.Call(values)
	return result, nil
}

// Close 关闭热更新管理器
func (hrm *HotReloadManager) Close() error {
	hrm.cancel()

	if hrm.watcher != nil {
		return hrm.watcher.Close()
	}

	return nil
}

// YAMLConfigParser YAML配置解析器
type YAMLConfigParser struct{}

// Parse 解析YAML配置
func (p *YAMLConfigParser) Parse(data []byte) (interface{}, error) {
	// 这里应该使用YAML库解析
	// 简化实现，返回字符串
	return string(data), nil
}

// Validate 验证配置
func (p *YAMLConfigParser) Validate(data interface{}) error {
	// 这里实现配置验证逻辑
	return nil
}

// JSONConfigParser JSON配置解析器
type JSONConfigParser struct{}

// Parse 解析JSON配置
func (p *JSONConfigParser) Parse(data []byte) (interface{}, error) {
	// 这里应该使用JSON库解析
	return string(data), nil
}

// Validate 验证配置
func (p *JSONConfigParser) Validate(data interface{}) error {
	return nil
}
