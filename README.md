<h1 align="center">🚀 Kubernetes Deployment Handler</h1>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-blue.svg?style=flat-square&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Kubernetes-client--go-green.svg?style=flat-square&logo=kubernetes" alt="Kubernetes">
  <img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="License">
</p>

<p align="center">
  <strong>一个基于 client-go 的 Kubernetes Deployment 定制化监控控制器</strong>
</p>

<p align="center">
  <img src="https://raw.githubusercontent.com/kubernetes/kubernetes/master/logo/logo.png" width="150" height="150">
</p>

---

## 📋 目录

- [🌟 特性](#-特性)
- [🔧 技术栈](#-技术栈)
- [📥 安装](#-安装)
- [⚙️ 配置](#️-配置)
- [🚀 使用方法](#-使用方法)
- [📄 许可证](#-许可证)

## 🌟 特性

- 🔍 **实时监控**: 监控指定命名空间下的所有 Deployment 资源
- ⚡ **事件响应**: 响应 Deployment 的增加、更新和删除事件
- 🛠️ **脚本执行**: 在 Deployment 事件发生时自动执行自定义脚本
- 🌐 **跨平台支持**: 支持 Windows 和 Linux/macOS 系统
- 🔧 **灵活配置**: 可轻松配置监控的命名空间和脚本路径
- 📦 **自动服务创建**: 为 Deployment 自动创建 NodePort 服务
- 🌱 **环境变量注入**: 自动为应用注入服务发现所需的环境变量

## 🔧 技术栈

| 技术 | 描述 | 版本 |
|------|------|------|
| ![Go](https://img.shields.io/badge/-Go-00ADD8?style=flat-square&logo=go&logoColor=white) | 编程语言 | 1.25 |
| ![Kubernetes](https://img.shields.io/badge/-Kubernetes-326CE5?style=flat-square&logo=kubernetes&logoColor=white) | 容器编排平台 | client-go v0.34.2 |
| ![Gin](https://img.shields.io/badge/-Gin-00B894?style=flat-square&logo=go&logoColor=white) | Web 框架 | v1.11.0 |

## 📥 安装

### 克隆项目

```bash
git clone https://github.com/your-username/kubernetes-deploy-handler.git
cd kubernetes-deploy-handler
```

### 构建项目

```bash
go build -o kubernetes-deploy-handler .
```

### 或者直接运行

```bash
go run main.go
```

## ⚙️ 配置

### 命名空间配置

在 [`pkg/controller.go`](file:///d:/workspaces/GolandProjects/kubernetes-deploy-handler/pkg/controller.go) 文件中修改监控的命名空间：

```go
const (
    Namespace = "your-target-namespace"  // 修改为目标命名空间
)
```

### 脚本配置

在 [`pkg/handler.go`](file:///d:/workspaces/GolandProjects/kubernetes-deploy-handler/pkg/handler.go) 文件中修改要执行的脚本路径：

```go
// 根据操作系统选择合适的脚本
var scriptPath string
if runtime.GOOS == "windows" {
    scriptPath = "path/to/your/script.bat"  // Windows 脚本
} else {
    scriptPath = "path/to/your/script.sh"   // Unix/Linux 脚本
}
```

## 🚀 使用方法

### 1. 准备脚本

创建您的自定义脚本，接收三个参数：
- `$1`: Deployment 名称
- `$2`: 命名空间
- `$3`: 事件类型 (add, update, delete)

示例脚本 ([example-script.sh](file:///d:/workspaces/GolandProjects/kubernetes-deploy-handler/example-script.sh)):
```bash

#!/bin/bash
DEPLOYMENT_NAME=$1
NAMESPACE=$2
EVENT_TYPE=$3

echo "处理 Deployment $DEPLOYMENT_NAME ($EVENT_TYPE) 事件"
# 在此处添加您的业务逻辑
```

### 2. 运行应用

本地运行基于
```bash
    go build -o controller ./cmd/controller
```

### 3. 观察日志

应用启动后会显示类似以下的日志信息：

```
===============================================
  🚀 NodePort Controller 启动中...
  📦 正在初始化 Kubernetes 客户端...
===============================================
```

### 4. 测试功能

在目标命名空间中创建、更新或删除 Deployment，观察应用是否会执行您的脚本：

```bash
# 创建 Deployment
kubectl create deployment test-app --image=nginx -n syncplant-backend

# 更新 Deployment
kubectl scale deployment test-app --replicas=3 -n syncplant-backend

# 删除 Deployment
kubectl delete deployment test-app -n syncplant-backend
```

## 🥗 Kubernetes 部署
```yaml
# 创建专门的 ServiceAccount，放在devops下
apiVersion: v1
kind: ServiceAccount
metadata:
  name: syncplant-controller-sa
  namespace: devops
---
# 创建 ClusterRole 因为操作跨 namespace 的 deployment/service/pod
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: syncplant-controller-cr
rules:
  - apiGroups: [ "apps" ]
    resources: [ "deployments" ]
    verbs: [ "get", "list", "watch", "update", "patch" ]

  - apiGroups: [ "" ]
    resources: [ "services", "pods" ]
    verbs: [ "get", "list", "watch", "create", "update", "patch" ]
---
# 使用 ClusterRoleBinding，把权限只绑定到这个 SA（安全）
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: syncplant-controller-crb
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: syncplant-controller-cr
subjects:
  - kind: ServiceAccount
    name: syncplant-controller-sa
    namespace: devops   # Controller 所在的 namespace
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: syncplant-svc-controller
  namespace: devops
spec:
  replicas: 1
  selector:
    matchLabels:
      app: syncplant-svc-controller
  template:
    metadata:
      labels:
        app: syncplant-svc-controller
    spec:
      serviceAccountName: syncplant-controller-sa
      containers:
        - name: controller
          image: bubua12/auto-config-controller:1.0.6
          imagePullPolicy: IfNotPresent
```

## 📄 许可证

MIT License - 查看 [LICENSE](LICENSE) 文件了解详情

---

<p align="center">
  Made with ❤️ by Kubernetes Developer
</p>