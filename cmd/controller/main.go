package main

import (
	"context"
	"kubernetes-deploy-handler/pkg"
	"log"
)

func main() {
	log.Println("NodePort Controller 启动中...")

	ctx := context.Background()

	if err := pkg.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
