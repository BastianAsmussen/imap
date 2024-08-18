package main

import (
	"bufio"
	"fmt"
	"github.com/go-ping/ping"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Ping sends an ICMP echo request to an IP address.
func Ping(ip string) (bool, time.Duration) {
	pinger, err := ping.NewPinger(ip)
	if err != nil {
		return false, 0
	}
	pinger.Count = 1
	pinger.Timeout = time.Second
	err = pinger.Run()
	stats := pinger.Statistics()

	if stats.PacketsRecv > 0 {
		return true, stats.AvgRtt
	}
	return false, 0
}

// WriteToWAL writes the current IP to the WAL file before pinging it.
func WriteToWAL(ip string) error {
	file, err := os.OpenFile("wal.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(ip + "\n")
	return err
}

// ReadLastIPFromWAL reads the last IP that was processed from the WAL file.
func ReadLastIPFromWAL() (net.IP, error) {
	file, err := os.Open("wal.log")
	if err != nil {
		if os.IsNotExist(err) {
			return net.IPv4(0, 0, 0, 0), nil // Start from 0.0.0.0 if WAL doesn't exist
		}
		return nil, err
	}
	defer file.Close()

	var lastIP string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lastIP = scanner.Text()
	}

	if lastIP == "" {
		return net.IPv4(0, 0, 0, 0), nil
	}

	return net.ParseIP(lastIP), nil
}

// ValidateIP ensures the IP is valid and not in a reserved range like 0.0.0.0/8.
func ValidateIP(ip net.IP) bool {
	// Check if IP is valid
	if ip == nil {
		return false
	}
	// Additional checks for specific ranges
	if ip.IsUnspecified() || ip[0] == 0 {
		return false
	}
	return true
}

// CacheResult caches the result of an IP ping to a file.
func CacheResult(ip string, reachable bool, responseTime time.Duration) {
	cacheDir := "cache"
	err := os.MkdirAll(cacheDir, os.ModePerm)
	if err != nil {
		fmt.Printf("Failed to create cache directory: %v\n", err)
		return
	}

	file, err := os.OpenFile(filepath.Join(cacheDir, "ping_results.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Failed to open cache file: %v\n", err)
		return
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	result := fmt.Sprintf("%s: %t, %v\n", ip, reachable, responseTime)
	_, err = writer.WriteString(result)
	if err != nil {
		fmt.Printf("Failed to write to cache file: %v\n", err)
	}
}

// nextIP calculates the next IP address.
func nextIP(ip net.IP) net.IP {
	ip = ip.To4()
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
	return ip
}

// ScanIPRange scans the entire IPv4 space and checks if each IP is reachable.
func ScanIPRange(startIP, endIP net.IP, maxWorkers int) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxWorkers)

	for ip := startIP; !ip.Equal(endIP); ip = nextIP(ip) {
		if !ValidateIP(ip) {
			continue
		}
		wg.Add(1)
		semaphore <- struct{}{} // block if maxWorkers is reached

		go func(ip string) {
			defer wg.Done()
			defer func() { <-semaphore }() // release worker slot

			if err := WriteToWAL(ip); err != nil {
				fmt.Printf("Failed to write to WAL: %v\n", err)
				return
			}

			reachable, elapsed := Ping(ip)
			CacheResult(ip, reachable, elapsed)
			if reachable {
				fmt.Printf("%s is reachable\n", ip)
			} else {
				fmt.Printf("%s is not reachable\n", ip)
			}
		}(ip.String())
	}

	wg.Wait()
	close(semaphore)
}

func main() {
	startIP, err := ReadLastIPFromWAL()
	if err != nil {
		fmt.Printf("Failed to read WAL: %v\n", err)
		return
	}

	endIP := net.ParseIP("255.255.255.255")
	maxWorkers := 8 * 1024

	start := time.Now()
	ScanIPRange(startIP, endIP, maxWorkers)
	fmt.Printf("Scan completed in %v\n", time.Since(start))
}
