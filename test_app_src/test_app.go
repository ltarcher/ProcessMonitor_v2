package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	// 健康检查端点
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	// 状态端点
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Service is running at %s", time.Now().Format("2006-01-02 15:04:05"))
	})

	// 根路径
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Test Application - Process Monitor Demo")
	})

	log.Println("Test application starting on port 8080...")
	log.Println("Health check: http://localhost:8080/health")
	log.Println("Status check: http://localhost:8080/status")
	
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}