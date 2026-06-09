package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// PerformanceAnalyzer 性能分析器
type PerformanceAnalyzer struct {
	services []ServiceEndpoint
	reports  []PerformanceReport
}

// ServiceEndpoint 服务端点
type ServiceEndpoint struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// PerformanceReport 性能报告
type PerformanceReport struct {
	ServiceName     string             `json:"service_name"`
	Timestamp       time.Time          `json:"timestamp"`
	Metrics         map[string]float64 `json:"metrics"`
	Alerts          []Alert            `json:"alerts"`
	Recommendations []string           `json:"recommendations"`
}

// Alert 告警信息
type Alert struct {
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Timestamp time.Time `json:"timestamp"`
}

// MetricsData 指标数据
type MetricsData struct {
	System struct {
		CPUPercent    []float64 `json:"cpu_percent"`
		MemoryUsed    uint64    `json:"memory_used"`
		MemoryTotal   uint64    `json:"memory_total"`
		MemoryPercent float64   `json:"memory_percent"`
	} `json:"system"`
	Runtime struct {
		Goroutines  int    `json:"goroutines"`
		HeapAlloc   uint64 `json:"heap_alloc"`
		HeapSys     uint64 `json:"heap_sys"`
		HeapObjects uint64 `json:"heap_objects"`
		GCCycles    uint32 `json:"gc_cycles"`
	} `json:"runtime"`
	Connections int `json:"connections,omitempty"`
	ActorCount  int `json:"actor_count,omitempty"`
}

// NewPerformanceAnalyzer 创建性能分析器
func NewPerformanceAnalyzer() *PerformanceAnalyzer {
	return &PerformanceAnalyzer{
		services: []ServiceEndpoint{
			{Name: "center", Address: "localhost", Port: 7010},
			{Name: "gateway1", Address: "localhost", Port: 7001},
			{Name: "gateway2", Address: "localhost", Port: 7002},
			{Name: "login", Address: "localhost", Port: 7020},
			{Name: "lobby", Address: "localhost", Port: 7030},
			{Name: "game1", Address: "localhost", Port: 7100},
			{Name: "game2", Address: "localhost", Port: 7101},
			{Name: "game3", Address: "localhost", Port: 7102},
			{Name: "friend", Address: "localhost", Port: 7040},
			{Name: "chat", Address: "localhost", Port: 7050},
			{Name: "mail", Address: "localhost", Port: 7060},
			{Name: "gm", Address: "localhost", Port: 7200},
		},
		reports: make([]PerformanceReport, 0),
	}
}

// CollectMetrics 收集所有服务的指标
func (pa *PerformanceAnalyzer) CollectMetrics() error {
	fmt.Println("开始收集性能指标...")

	for _, service := range pa.services {
		fmt.Printf("收集 %s 服务指标...\n", service.Name)

		report, err := pa.analyzeService(service)
		if err != nil {
			fmt.Printf("  ⚠️  %s: %v\n", service.Name, err)
			continue
		}

		pa.reports = append(pa.reports, report)
		pa.displayServiceReport(report)
	}

	return nil
}

// analyzeService 分析单个服务
func (pa *PerformanceAnalyzer) analyzeService(service ServiceEndpoint) (PerformanceReport, error) {
	url := fmt.Sprintf("http://%s:%d/api/metrics", service.Address, service.Port)

	// 获取指标数据
	resp, err := http.Get(url)
	if err != nil {
		return PerformanceReport{}, fmt.Errorf("无法连接到服务: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return PerformanceReport{}, fmt.Errorf("无法读取响应: %v", err)
	}

	var metrics MetricsData
	if err := json.Unmarshal(body, &metrics); err != nil {
		return PerformanceReport{}, fmt.Errorf("无法解析指标数据: %v", err)
	}

	// 分析指标并生成报告
	report := PerformanceReport{
		ServiceName:     service.Name,
		Timestamp:       time.Now(),
		Metrics:         make(map[string]float64),
		Alerts:          make([]Alert, 0),
		Recommendations: make([]string, 0),
	}

	// 提取关键指标
	if len(metrics.System.CPUPercent) > 0 {
		report.Metrics["cpu_percent"] = metrics.System.CPUPercent[0]
	}
	report.Metrics["memory_percent"] = metrics.System.MemoryPercent
	report.Metrics["goroutines"] = float64(metrics.Runtime.Goroutines)
	report.Metrics["heap_alloc_mb"] = float64(metrics.Runtime.HeapAlloc) / 1024 / 1024
	report.Metrics["heap_objects"] = float64(metrics.Runtime.HeapObjects)

	if metrics.Connections > 0 {
		report.Metrics["connections"] = float64(metrics.Connections)
	}
	if metrics.ActorCount > 0 {
		report.Metrics["actors"] = float64(metrics.ActorCount)
	}

	// 分析告警
	pa.analyzeAlerts(&report)

	// 生成建议
	pa.generateRecommendations(&report)

	return report, nil
}

// analyzeAlerts 分析告警
func (pa *PerformanceAnalyzer) analyzeAlerts(report *PerformanceReport) {
	// CPU使用率告警
	if cpuPercent, exists := report.Metrics["cpu_percent"]; exists && cpuPercent > 80 {
		report.Alerts = append(report.Alerts, Alert{
			Level:     "warning",
			Message:   "CPU使用率过高",
			Metric:    "cpu_percent",
			Value:     cpuPercent,
			Threshold: 80,
			Timestamp: time.Now(),
		})
	}

	// 内存使用率告警
	if memPercent, exists := report.Metrics["memory_percent"]; exists && memPercent > 85 {
		report.Alerts = append(report.Alerts, Alert{
			Level:     "warning",
			Message:   "内存使用率过高",
			Metric:    "memory_percent",
			Value:     memPercent,
			Threshold: 85,
			Timestamp: time.Now(),
		})
	}

	// Goroutine数量告警
	if goroutines, exists := report.Metrics["goroutines"]; exists && goroutines > 10000 {
		report.Alerts = append(report.Alerts, Alert{
			Level:     "critical",
			Message:   "Goroutine数量异常",
			Metric:    "goroutines",
			Value:     goroutines,
			Threshold: 10000,
			Timestamp: time.Now(),
		})
	}

	// 连接数告警
	if connections, exists := report.Metrics["connections"]; exists && connections > 8000 {
		report.Alerts = append(report.Alerts, Alert{
			Level:     "warning",
			Message:   "连接数接近上限",
			Metric:    "connections",
			Value:     connections,
			Threshold: 8000,
			Timestamp: time.Now(),
		})
	}
}

// generateRecommendations 生成优化建议
func (pa *PerformanceAnalyzer) generateRecommendations(report *PerformanceReport) {
	// CPU优化建议
	if cpuPercent, exists := report.Metrics["cpu_percent"]; exists {
		if cpuPercent > 80 {
			report.Recommendations = append(report.Recommendations,
				"CPU使用率较高，建议检查热点函数并优化算法")
		} else if cpuPercent > 60 {
			report.Recommendations = append(report.Recommendations,
				"CPU使用率中等，建议进行性能分析")
		}
	}

	// 内存优化建议
	if heapAllocMB, exists := report.Metrics["heap_alloc_mb"]; exists {
		if heapAllocMB > 512 {
			report.Recommendations = append(report.Recommendations,
				"堆内存使用较高，建议检查内存泄漏并优化对象池使用")
		}
	}

	// Goroutine优化建议
	if goroutines, exists := report.Metrics["goroutines"]; exists {
		if goroutines > 5000 {
			report.Recommendations = append(report.Recommendations,
				"Goroutine数量较多，建议检查是否存在goroutine泄漏")
		}
	}

	// 连接数优化建议
	if connections, exists := report.Metrics["connections"]; exists {
		if connections > 5000 {
			report.Recommendations = append(report.Recommendations,
				"连接数较高，建议优化连接管理和增加连接池")
		}
	}
}

// displayServiceReport 显示服务报告
func (pa *PerformanceAnalyzer) displayServiceReport(report PerformanceReport) {
	fmt.Printf("  📊 %s 性能报告:\n", report.ServiceName)

	// 显示关键指标
	fmt.Printf("    CPU: %.1f%% | 内存: %.1f%% | Goroutines: %.0f\n",
		report.Metrics["cpu_percent"],
		report.Metrics["memory_percent"],
		report.Metrics["goroutines"])

	if heapMB, exists := report.Metrics["heap_alloc_mb"]; exists {
		fmt.Printf("    堆内存: %.1fMB | 堆对象: %.0f\n",
			heapMB, report.Metrics["heap_objects"])
	}

	if connections, exists := report.Metrics["connections"]; exists {
		fmt.Printf("    连接数: %.0f", connections)
		if actors, actorExists := report.Metrics["actors"]; actorExists {
			fmt.Printf(" | Actor数: %.0f", actors)
		}
		fmt.Println()
	}

	// 显示告警
	if len(report.Alerts) > 0 {
		fmt.Printf("    ⚠️  告警 (%d条):\n", len(report.Alerts))
		for _, alert := range report.Alerts {
			fmt.Printf("      [%s] %s (%.1f > %.1f)\n",
				strings.ToUpper(alert.Level), alert.Message, alert.Value, alert.Threshold)
		}
	}

	// 显示建议
	if len(report.Recommendations) > 0 {
		fmt.Printf("    💡 优化建议:\n")
		for _, rec := range report.Recommendations {
			fmt.Printf("      - %s\n", rec)
		}
	}

	fmt.Println()
}

// GenerateSummaryReport 生成汇总报告
func (pa *PerformanceAnalyzer) GenerateSummaryReport() {
	if len(pa.reports) == 0 {
		fmt.Println("没有性能数据可用于生成报告")
		return
	}

	fmt.Println("=== 集群性能汇总报告 ===")
	fmt.Println()

	// 汇总指标
	totalServices := len(pa.reports)
	totalAlerts := 0
	totalRecommendations := 0

	var totalCPU, totalMemory, totalGoroutines float64

	for _, report := range pa.reports {
		totalAlerts += len(report.Alerts)
		totalRecommendations += len(report.Recommendations)

		if cpu, exists := report.Metrics["cpu_percent"]; exists {
			totalCPU += cpu
		}
		if memory, exists := report.Metrics["memory_percent"]; exists {
			totalMemory += memory
		}
		if goroutines, exists := report.Metrics["goroutines"]; exists {
			totalGoroutines += goroutines
		}
	}

	fmt.Printf("📈 集群概览:\n")
	fmt.Printf("  服务总数: %d\n", totalServices)
	fmt.Printf("  平均CPU使用率: %.1f%%\n", totalCPU/float64(totalServices))
	fmt.Printf("  平均内存使用率: %.1f%%\n", totalMemory/float64(totalServices))
	fmt.Printf("  总Goroutines: %.0f\n", totalGoroutines)
	fmt.Printf("  告警总数: %d\n", totalAlerts)
	fmt.Printf("  优化建议: %d条\n", totalRecommendations)
	fmt.Println()

	// 按CPU使用率排序服务
	sort.Slice(pa.reports, func(i, j int) bool {
		return pa.reports[i].Metrics["cpu_percent"] > pa.reports[j].Metrics["cpu_percent"]
	})

	fmt.Println("🔥 CPU使用率排行:")
	for i, report := range pa.reports {
		if i >= 5 { // 只显示前5名
			break
		}
		fmt.Printf("  %d. %s: %.1f%%\n", i+1, report.ServiceName, report.Metrics["cpu_percent"])
	}
	fmt.Println()

	// 显示所有告警
	if totalAlerts > 0 {
		fmt.Println("🚨 集群告警:")
		for _, report := range pa.reports {
			for _, alert := range report.Alerts {
				fmt.Printf("  [%s] %s - %s: %.1f\n",
					strings.ToUpper(alert.Level), report.ServiceName, alert.Message, alert.Value)
			}
		}
		fmt.Println()
	}

	// 汇总优化建议
	if totalRecommendations > 0 {
		fmt.Println("💡 集群优化建议:")
		recommendationMap := make(map[string]int)

		for _, report := range pa.reports {
			for _, rec := range report.Recommendations {
				recommendationMap[rec]++
			}
		}

		for rec, count := range recommendationMap {
			fmt.Printf("  (%d个服务) %s\n", count, rec)
		}
	}
}

// SaveReport 保存报告到文件
func (pa *PerformanceAnalyzer) SaveReport(filename string) error {
	data, err := json.MarshalIndent(pa.reports, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %v", err)
	}

	return ioutil.WriteFile(filename, data, 0644)
}

// LoadReport 从文件加载报告
func (pa *PerformanceAnalyzer) LoadReport(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read report file: %v", err)
	}

	return json.Unmarshal(data, &pa.reports)
}

// CompareReports 比较两次报告
func (pa *PerformanceAnalyzer) CompareReports(oldReportFile string) error {
	oldAnalyzer := NewPerformanceAnalyzer()
	if err := oldAnalyzer.LoadReport(oldReportFile); err != nil {
		return fmt.Errorf("failed to load old report: %v", err)
	}

	fmt.Println("=== 性能对比报告 ===")
	fmt.Println()

	// 按服务名匹配并比较
	oldReportMap := make(map[string]PerformanceReport)
	for _, report := range oldAnalyzer.reports {
		oldReportMap[report.ServiceName] = report
	}

	for _, newReport := range pa.reports {
		if oldReport, exists := oldReportMap[newReport.ServiceName]; exists {
			pa.compareServiceMetrics(newReport, oldReport)
		}
	}

	return nil
}

// compareServiceMetrics 比较服务指标
func (pa *PerformanceAnalyzer) compareServiceMetrics(newReport, oldReport PerformanceReport) {
	fmt.Printf("📊 %s 性能对比:\n", newReport.ServiceName)

	metrics := []string{"cpu_percent", "memory_percent", "goroutines", "heap_alloc_mb"}

	for _, metric := range metrics {
		newValue, newExists := newReport.Metrics[metric]
		oldValue, oldExists := oldReport.Metrics[metric]

		if newExists && oldExists {
			change := newValue - oldValue
			changePercent := (change / oldValue) * 100

			icon := "📊"
			if changePercent > 10 {
				icon = "📈"
			} else if changePercent < -10 {
				icon = "📉"
			}

			fmt.Printf("  %s %s: %.1f -> %.1f (%.1f%%)\n",
				icon, metric, oldValue, newValue, changePercent)
		}
	}

	fmt.Println()
}

// GeneratePprofReport 生成pprof分析报告
func (pa *PerformanceAnalyzer) GeneratePprofReport() error {
	fmt.Println("=== pprof 性能分析 ===")
	fmt.Println()

	pprofEndpoints := []struct {
		name string
		path string
		desc string
	}{
		{"heap", "/debug/pprof/heap", "堆内存分析"},
		{"profile", "/debug/pprof/profile?seconds=30", "CPU性能分析（30秒）"},
		{"goroutine", "/debug/pprof/goroutine", "Goroutine分析"},
		{"allocs", "/debug/pprof/allocs", "内存分配分析"},
		{"block", "/debug/pprof/block", "阻塞分析"},
		{"mutex", "/debug/pprof/mutex", "锁竞争分析"},
	}

	fmt.Println("🔬 可用的pprof分析命令:")
	fmt.Println()

	for _, service := range pa.services {
		pprofPort := service.Port + 1000 // pprof端口偏移
		fmt.Printf("📍 %s 服务 (:%d):\n", service.Name, pprofPort)

		for _, endpoint := range pprofEndpoints {
			url := fmt.Sprintf("http://%s:%d%s", service.Address, pprofPort, endpoint.path)
			fmt.Printf("  %s: go tool pprof %s\n", endpoint.desc, url)
		}
		fmt.Println()
	}

	fmt.Println("💡 pprof 使用提示:")
	fmt.Println("  1. 使用 'top' 查看CPU热点函数")
	fmt.Println("  2. 使用 'list 函数名' 查看函数详细信息")
	fmt.Println("  3. 使用 'web' 生成调用图（需要graphviz）")
	fmt.Println("  4. 使用 'png' 生成PNG调用图")
	fmt.Println()

	return nil
}

// main 主函数
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run performance_analyzer.go [command]")
		fmt.Println("Commands:")
		fmt.Println("  collect              - 收集当前性能指标")
		fmt.Println("  compare [old_report] - 与历史报告对比")
		fmt.Println("  pprof               - 生成pprof分析命令")
		fmt.Println("  save [filename]     - 保存报告到文件")
		fmt.Println("  watch               - 实时监控模式")
		return
	}

	analyzer := NewPerformanceAnalyzer()
	command := os.Args[1]

	switch command {
	case "collect":
		if err := analyzer.CollectMetrics(); err != nil {
			fmt.Printf("收集指标失败: %v\n", err)
			os.Exit(1)
		}
		analyzer.GenerateSummaryReport()

	case "compare":
		if len(os.Args) < 3 {
			fmt.Println("请指定历史报告文件")
			os.Exit(1)
		}

		if err := analyzer.CollectMetrics(); err != nil {
			fmt.Printf("收集指标失败: %v\n", err)
			os.Exit(1)
		}

		if err := analyzer.CompareReports(os.Args[2]); err != nil {
			fmt.Printf("对比报告失败: %v\n", err)
			os.Exit(1)
		}

	case "pprof":
		if err := analyzer.GeneratePprofReport(); err != nil {
			fmt.Printf("生成pprof报告失败: %v\n", err)
			os.Exit(1)
		}

	case "save":
		filename := "performance_report.json"
		if len(os.Args) >= 3 {
			filename = os.Args[2]
		}

		if err := analyzer.CollectMetrics(); err != nil {
			fmt.Printf("收集指标失败: %v\n", err)
			os.Exit(1)
		}

		if err := analyzer.SaveReport(filename); err != nil {
			fmt.Printf("保存报告失败: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("报告已保存到: %s\n", filename)

	case "watch":
		fmt.Println("启动实时监控模式（按Ctrl+C退出）...")

		for {
			fmt.Print("\033[H\033[2J") // 清屏
			fmt.Printf("🕐 %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

			analyzer.reports = nil // 清空之前的报告
			if err := analyzer.CollectMetrics(); err != nil {
				fmt.Printf("收集指标失败: %v\n", err)
			} else {
				analyzer.GenerateSummaryReport()
			}

			time.Sleep(10 * time.Second)
		}

	default:
		fmt.Printf("未知命令: %s\n", command)
		os.Exit(1)
	}
}
