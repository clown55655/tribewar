package monitoring

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"

	"tribeway/internal/logger"
)

// MonitoringManager 监控管理器
type MonitoringManager struct {
	registry   *prometheus.Registry
	httpServer *http.Server
	ginEngine  *gin.Engine
	alerts     *AlertManager
	metrics    *MetricsCollector
	ctx        context.Context
	cancel     context.CancelFunc
	nodeID     string
	nodeType   string
	options    MonitoringOptions
}

// MonitoringOptions controls exposure of monitoring and management endpoints.
type MonitoringOptions struct {
	BindAddress            string
	AdminTokenEnv          string
	AllowedCIDRs           []string
	ProtectMetricsEndpoint bool
}

// MetricsCollector 指标收集器
type MetricsCollector struct {
	// 系统指标
	cpuUsage    *prometheus.GaugeVec
	memoryUsage *prometheus.GaugeVec
	goroutines  *prometheus.GaugeVec
	heapSize    *prometheus.GaugeVec
	heapObjects *prometheus.GaugeVec
	gcDuration  *prometheus.SummaryVec

	// 业务指标
	connectionCount *prometheus.GaugeVec
	actorCount      *prometheus.GaugeVec
	messageCount    *prometheus.CounterVec
	errorCount      *prometheus.CounterVec
	requestDuration *prometheus.SummaryVec
	dbConnections   *prometheus.GaugeVec

	// 自定义指标
	customMetrics map[string]prometheus.Metric
	mutex         sync.RWMutex
}

// AlertManager 告警管理器
type AlertManager struct {
	rules    []AlertRule
	channels []AlertChannel
	history  []Alert
	mutex    sync.RWMutex
}

// AlertRule 告警规则
type AlertRule struct {
	Name      string
	Condition func() bool
	Message   string
	Level     AlertLevel
	Cooldown  time.Duration
	LastAlert time.Time
}

// AlertChannel 告警通道
type AlertChannel interface {
	Send(alert Alert) error
}

// Alert 告警
type Alert struct {
	ID        string
	Rule      string
	Level     AlertLevel
	Message   string
	Timestamp time.Time
	NodeID    string
	NodeType  string
}

// AlertLevel 告警级别
type AlertLevel int

const (
	AlertLevelInfo AlertLevel = iota
	AlertLevelWarning
	AlertLevelError
	AlertLevelCritical
)

// NewMonitoringManager 创建监控管理器
func NewMonitoringManager(nodeID, nodeType string, port int) (*MonitoringManager, error) {
	return NewMonitoringManagerWithOptions(nodeID, nodeType, port, MonitoringOptions{})
}

func NewMonitoringManagerWithOptions(nodeID, nodeType string, port int, options MonitoringOptions) (*MonitoringManager, error) {
	registry := prometheus.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())

	// 创建Gin引擎
	gin.SetMode(gin.ReleaseMode)
	ginEngine := gin.New()
	ginEngine.Use(gin.Recovery())

	// 创建指标收集器
	metricsCollector, err := NewMetricsCollector(nodeID, nodeType)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create metrics collector: %v", err)
	}

	// 注册指标
	registry.MustRegister(metricsCollector)

	// 创建告警管理器
	alertManager := NewAlertManager()

	manager := &MonitoringManager{
		registry:  registry,
		ginEngine: ginEngine,
		alerts:    alertManager,
		metrics:   metricsCollector,
		ctx:       ctx,
		cancel:    cancel,
		nodeID:    nodeID,
		nodeType:  nodeType,
		options:   normalizeMonitoringOptions(options),
	}

	// 设置HTTP服务器
	manager.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", manager.options.BindAddress, port),
		Handler:           ginEngine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 注册路由
	manager.registerRoutes()

	// 启动指标收集
	go manager.collectMetrics()

	// 启动告警检查
	go manager.checkAlerts()

	logger.Info(fmt.Sprintf("Monitoring manager initialized on %s", manager.httpServer.Addr))
	return manager, nil
}

func normalizeMonitoringOptions(options MonitoringOptions) MonitoringOptions {
	if options.BindAddress == "" {
		options.BindAddress = "127.0.0.1"
	}
	if options.AdminTokenEnv == "" {
		options.AdminTokenEnv = "TRIBEWAY_MONITORING_ADMIN_TOKEN"
	}
	if len(options.AllowedCIDRs) == 0 {
		options.AllowedCIDRs = []string{"127.0.0.1/32", "::1/128"}
	}
	return options
}

// NewMetricsCollector 创建指标收集器
func NewMetricsCollector(nodeID, nodeType string) (*MetricsCollector, error) {

	return &MetricsCollector{
		cpuUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tribeway_cpu_usage_percent",
				Help: "Current CPU usage percentage",
			},
			[]string{"node_id", "node_type"},
		),

		memoryUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tribeway_memory_usage_bytes",
				Help: "Current memory usage in bytes",
			},
			[]string{"node_id", "node_type"},
		),

		goroutines: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tribeway_goroutines_total",
				Help: "Current number of goroutines",
			},
			[]string{"node_id", "node_type"},
		),

		heapSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tribeway_heap_size_bytes",
				Help: "Current heap size in bytes",
			},
			[]string{"node_id", "node_type"},
		),

		heapObjects: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tribeway_heap_objects_total",
				Help: "Current number of heap objects",
			},
			[]string{"node_id", "node_type"},
		),

		gcDuration: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name: "tribeway_gc_duration_seconds",
				Help: "Time spent in garbage collection",
			},
			[]string{"node_id", "node_type"},
		),

		connectionCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tribeway_connections_total",
				Help: "Current number of active connections",
			},
			[]string{"node_id", "node_type"},
		),

		actorCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tribeway_actors_total",
				Help: "Current number of active actors",
			},
			[]string{"node_id", "node_type"},
		),

		messageCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tribeway_messages_total",
				Help: "Total number of messages processed",
			},
			[]string{"node_id", "node_type", "message_type"},
		),

		errorCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tribeway_errors_total",
				Help: "Total number of errors",
			},
			[]string{"node_id", "node_type", "error_type"},
		),

		requestDuration: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name: "tribeway_request_duration_seconds",
				Help: "Request duration in seconds",
			},
			[]string{"node_id", "node_type", "method", "endpoint"},
		),

		customMetrics: make(map[string]prometheus.Metric),
	}, nil
}

// Describe 实现prometheus.Collector接口
func (mc *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	mc.cpuUsage.Describe(ch)
	mc.memoryUsage.Describe(ch)
	mc.goroutines.Describe(ch)
	mc.heapSize.Describe(ch)
	mc.heapObjects.Describe(ch)
	mc.gcDuration.Describe(ch)
	mc.connectionCount.Describe(ch)
	mc.actorCount.Describe(ch)
	mc.messageCount.Describe(ch)
	mc.errorCount.Describe(ch)
	mc.requestDuration.Describe(ch)
}

// Collect 实现prometheus.Collector接口
func (mc *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	mc.cpuUsage.Collect(ch)
	mc.memoryUsage.Collect(ch)
	mc.goroutines.Collect(ch)
	mc.heapSize.Collect(ch)
	mc.heapObjects.Collect(ch)
	mc.gcDuration.Collect(ch)
	mc.connectionCount.Collect(ch)
	mc.actorCount.Collect(ch)
	mc.messageCount.Collect(ch)
	mc.errorCount.Collect(ch)
	mc.requestDuration.Collect(ch)

	// 收集自定义指标
	mc.mutex.RLock()
	for _, metric := range mc.customMetrics {
		ch <- metric
	}
	mc.mutex.RUnlock()
}

// registerRoutes 注册路由
func (mm *MonitoringManager) registerRoutes() {
	adminOnly := mm.adminAuthMiddleware()

	// Prometheus metrics endpoint
	metricsHandlers := []gin.HandlerFunc{gin.WrapH(promhttp.HandlerFor(mm.registry, promhttp.HandlerOpts{}))}
	if mm.options.ProtectMetricsEndpoint {
		metricsHandlers = append([]gin.HandlerFunc{adminOnly}, metricsHandlers...)
	}
	mm.ginEngine.GET("/metrics", metricsHandlers...)

	// pprof endpoints
	mm.ginEngine.GET("/debug/pprof/", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/cmdline", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/profile", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/symbol", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/trace", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/heap", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/goroutine", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/allocs", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/block", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))
	mm.ginEngine.GET("/debug/pprof/mutex", adminOnly, gin.WrapF(http.DefaultServeMux.ServeHTTP))

	// 健康检查
	mm.ginEngine.GET("/health", mm.healthCheck)

	// 指标查询
	mm.ginEngine.GET("/api/metrics", adminOnly, mm.getMetrics)

	// 告警信息
	mm.ginEngine.GET("/api/alerts", adminOnly, mm.getAlerts)

	// 系统信息
	mm.ginEngine.GET("/api/system", adminOnly, mm.getSystemInfo)
}

func (mm *MonitoringManager) adminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !mm.remoteAddrAllowed(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "source ip is not allowed"})
			return
		}

		expected := os.Getenv(mm.options.AdminTokenEnv)
		if expected == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "monitoring admin token is not configured"})
			return
		}

		token := c.GetHeader("X-Admin-Token")
		if token == "" {
			token = c.Query("token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
			return
		}

		c.Next()
	}
}

func (mm *MonitoringManager) remoteAddrAllowed(clientIP string) bool {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}
	for _, cidr := range mm.options.AllowedCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			logger.Warn(fmt.Sprintf("Invalid monitoring allowed CIDR ignored: %s", cidr))
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Start 启动监控服务
func (mm *MonitoringManager) Start() error {
	go func() {
		if err := mm.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(fmt.Sprintf("Monitoring server error: %v", err))
		}
	}()

	logger.Info(fmt.Sprintf("Monitoring server started on %s", mm.httpServer.Addr))
	return nil
}

// Stop 停止监控服务
func (mm *MonitoringManager) Stop() error {
	mm.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return mm.httpServer.Shutdown(ctx)
}

// collectMetrics 收集指标
func (mm *MonitoringManager) collectMetrics() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mm.updateSystemMetrics()
		case <-mm.ctx.Done():
			return
		}
	}
}

// updateSystemMetrics 更新系统指标
func (mm *MonitoringManager) updateSystemMetrics() {
	// CPU使用率
	if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
		mm.metrics.cpuUsage.WithLabelValues(mm.nodeID, mm.nodeType).Set(cpuPercent[0])
	}

	// 内存使用
	if memInfo, err := mem.VirtualMemory(); err == nil {
		mm.metrics.memoryUsage.WithLabelValues(mm.nodeID, mm.nodeType).Set(float64(memInfo.Used))
	}

	// Goroutine数量
	mm.metrics.goroutines.WithLabelValues(mm.nodeID, mm.nodeType).Set(float64(runtime.NumGoroutine()))

	// 堆内存信息
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	mm.metrics.heapSize.WithLabelValues(mm.nodeID, mm.nodeType).Set(float64(memStats.HeapSys))
	mm.metrics.heapObjects.WithLabelValues(mm.nodeID, mm.nodeType).Set(float64(memStats.HeapObjects))

	// GC信息
	if memStats.NumGC > 0 {
		mm.metrics.gcDuration.WithLabelValues(mm.nodeID, mm.nodeType).Observe(float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e9)
	}
}

// healthCheck 健康检查
func (mm *MonitoringManager) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"node_id":   mm.nodeID,
		"node_type": mm.nodeType,
		"timestamp": time.Now().Unix(),
	})
}

// getMetrics 获取指标
func (mm *MonitoringManager) getMetrics(c *gin.Context) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	cpuPercent, _ := cpu.Percent(0, false)
	memInfo, _ := mem.VirtualMemory()

	metrics := map[string]interface{}{
		"system": map[string]interface{}{
			"cpu_percent":    cpuPercent,
			"memory_used":    memInfo.Used,
			"memory_total":   memInfo.Total,
			"memory_percent": memInfo.UsedPercent,
		},
		"runtime": map[string]interface{}{
			"goroutines":   runtime.NumGoroutine(),
			"heap_alloc":   memStats.HeapAlloc,
			"heap_sys":     memStats.HeapSys,
			"heap_objects": memStats.HeapObjects,
			"gc_cycles":    memStats.NumGC,
		},
	}

	c.JSON(http.StatusOK, metrics)
}

// getAlerts 获取告警信息
func (mm *MonitoringManager) getAlerts(c *gin.Context) {
	alerts := mm.alerts.GetRecentAlerts(100)
	c.JSON(http.StatusOK, gin.H{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// getSystemInfo 获取系统信息
func (mm *MonitoringManager) getSystemInfo(c *gin.Context) {
	pid := int32(os.Getpid())
	proc, _ := process.NewProcess(pid)

	systemInfo := map[string]interface{}{
		"node_id":    mm.nodeID,
		"node_type":  mm.nodeType,
		"go_version": runtime.Version(),
		"go_os":      runtime.GOOS,
		"go_arch":    runtime.GOARCH,
		"start_time": time.Now().Unix(), // 应该是实际启动时间
	}

	if proc != nil {
		if createTime, err := proc.CreateTime(); err == nil {
			systemInfo["process_start_time"] = createTime / 1000
		}
		if cmdline, err := proc.Cmdline(); err == nil {
			systemInfo["cmdline"] = cmdline
		}
	}

	c.JSON(http.StatusOK, systemInfo)
}

// RecordMessage 记录消息指标
func (mm *MonitoringManager) RecordMessage(messageType string) {
	mm.metrics.messageCount.WithLabelValues(mm.nodeID, mm.nodeType, messageType).Inc()
}

// RecordError 记录错误指标
func (mm *MonitoringManager) RecordError(errorType string) {
	mm.metrics.errorCount.WithLabelValues(mm.nodeID, mm.nodeType, errorType).Inc()
}

// RecordRequestDuration 记录请求时长
func (mm *MonitoringManager) RecordRequestDuration(method, endpoint string, duration time.Duration) {
	mm.metrics.requestDuration.WithLabelValues(mm.nodeID, mm.nodeType, method, endpoint).Observe(duration.Seconds())
}

// SetConnectionCount 设置连接数
func (mm *MonitoringManager) SetConnectionCount(count int) {
	mm.metrics.connectionCount.WithLabelValues(mm.nodeID, mm.nodeType).Set(float64(count))
}

// SetActorCount 设置Actor数量
func (mm *MonitoringManager) SetActorCount(count int) {
	mm.metrics.actorCount.WithLabelValues(mm.nodeID, mm.nodeType).Set(float64(count))
}

// NewAlertManager 创建告警管理器
func NewAlertManager() *AlertManager {
	return &AlertManager{
		rules:    make([]AlertRule, 0),
		channels: make([]AlertChannel, 0),
		history:  make([]Alert, 0),
	}
}

// AddRule 添加告警规则
func (am *AlertManager) AddRule(rule AlertRule) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	am.rules = append(am.rules, rule)
}

// AddChannel 添加告警通道
func (am *AlertManager) AddChannel(channel AlertChannel) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	am.channels = append(am.channels, channel)
}

// checkAlerts 检查告警
func (mm *MonitoringManager) checkAlerts() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mm.alerts.CheckRules(mm.nodeID, mm.nodeType)
		case <-mm.ctx.Done():
			return
		}
	}
}

// CheckRules 检查告警规则
func (am *AlertManager) CheckRules(nodeID, nodeType string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	now := time.Now()

	for i, rule := range am.rules {
		// 检查冷却时间
		if now.Sub(rule.LastAlert) < rule.Cooldown {
			continue
		}

		// 检查条件
		if rule.Condition() {
			alert := Alert{
				ID:        fmt.Sprintf("%s_%d", rule.Name, now.Unix()),
				Rule:      rule.Name,
				Level:     rule.Level,
				Message:   rule.Message,
				Timestamp: now,
				NodeID:    nodeID,
				NodeType:  nodeType,
			}

			// 发送告警
			for _, channel := range am.channels {
				if err := channel.Send(alert); err != nil {
					logger.Error(fmt.Sprintf("Failed to send alert: %v", err))
				}
			}

			// 记录告警
			am.history = append(am.history, alert)
			am.rules[i].LastAlert = now

			logger.Warn(fmt.Sprintf("Alert triggered: %s - %s", rule.Name, rule.Message))
		}
	}
}

// GetRecentAlerts 获取最近的告警
func (am *AlertManager) GetRecentAlerts(limit int) []Alert {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	if len(am.history) <= limit {
		return am.history
	}

	return am.history[len(am.history)-limit:]
}

// LogAlertChannel 日志告警通道
type LogAlertChannel struct{}

// Send 发送告警到日志
func (lac *LogAlertChannel) Send(alert Alert) error {
	logger.Warn(fmt.Sprintf("[ALERT] %s: %s (Node: %s/%s)",
		alert.Rule, alert.Message, alert.NodeType, alert.NodeID))
	return nil
}
