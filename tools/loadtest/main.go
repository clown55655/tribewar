package main

import (
	"flag"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"tribeway/internal/network"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8001", "tcp server address")
	connections := flag.Int("connections", 100, "concurrent connections")
	requests := flag.Int("requests", 1000, "requests per connection")
	payloadSize := flag.Int("payload", 128, "payload size bytes")
	timeout := flag.Duration("timeout", 5*time.Second, "read/write timeout")
	flag.Parse()

	payload := make([]byte, *payloadSize)
	var success uint64
	var failed uint64
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(*connections)
	for i := 0; i < *connections; i++ {
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", *address, *timeout)
			if err != nil {
				atomic.AddUint64(&failed, uint64(*requests))
				return
			}
			defer conn.Close()

			for j := 0; j < *requests; j++ {
				err := network.WriteFrameWithOptions(conn, payload, network.FrameOptions{
					MaxFrameSize: network.DefaultMaxFrame,
					WriteTimeout: *timeout,
				})
				if err != nil {
					atomic.AddUint64(&failed, 1)
					continue
				}
				atomic.AddUint64(&success, 1)
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	total := atomic.LoadUint64(&success) + atomic.LoadUint64(&failed)
	qps := float64(total) / elapsed.Seconds()

	fmt.Printf("address=%s connections=%d requests_per_connection=%d total=%d success=%d failed=%d elapsed=%s qps=%.2f\n",
		*address,
		*connections,
		*requests,
		total,
		atomic.LoadUint64(&success),
		atomic.LoadUint64(&failed),
		elapsed,
		qps,
	)
}
