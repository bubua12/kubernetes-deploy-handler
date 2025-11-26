package main

import (
	"context"
	"fmt"
	"kubernetes-deploy-handler/pkg"
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	fmt.Println("==============> kubernetes start <==============")

	// 初始化gin对象
	r := gin.Default()

	// 启动 Deployment 监控控制器（在单独的 goroutine 中）
	go func() {
		fmt.Println("============> 启动 Deployment 监控控制器 <============")
		ctx := context.Background()
		if err := pkg.Run(ctx); err != nil {
			log.Printf("Deployment 控制器启动失败: %v", err)
		}
	}()

	// 简单的健康检查端点
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	// 启动服务
	fmt.Println("============> kubernetes go start running <============")
	err := r.Run(":8080") // 使用默认端口 8080
	if err != nil {
		log.Fatalf("Gin 启动失败: %v", err)
		return
	}
}