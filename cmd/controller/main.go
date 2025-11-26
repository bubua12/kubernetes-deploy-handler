package main

import (
	"context"
	"kubernetes-deploy-handler/pkg"
	"log"
)

func main() {
	log.Println("===============================================")
	log.Println("  🚀 NodePort Controller 启动中...")
	log.Println("  📦 正在初始化 Kubernetes 客户端...")
	log.Println("===============================================")

	ctx := context.Background()

	if err := pkg.Run(ctx); err != nil {
		log.Fatal("❌ Controller 启动失败: ", err)
	}
}
