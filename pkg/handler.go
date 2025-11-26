package pkg

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type DeploymentHandler struct {
	client *kubernetes.Clientset
}

func newDeploymentHandler(client *kubernetes.Clientset) *DeploymentHandler {
	return &DeploymentHandler{client: client}
}

// OnAdd 监听到添加事件时的处理逻辑，目前对于开发环境的自动配置SVC只关注这一个
func (h *DeploymentHandler) OnAdd(obj interface{}, _ bool) {
	dep := obj.(*appsv1.Deployment)
	log.Printf("[ADD] 检测到 Deployment 新增: %s/%s\n", dep.Namespace, dep.Name)
	h.handle(dep, "add")
}

func (h *DeploymentHandler) OnUpdate(oldObj, newObj interface{}) {
	// 这里不关心更新的操作
	//dep := newObj.(*appsv1.Deployment)
	//log.Printf("[UPDATE] 检测到 Deployment 更新: %s/%s\n", dep.Namespace, dep.Name)
	//h.handle(dep, "update")
}

func (h *DeploymentHandler) OnDelete(obj interface{}) {
	// 对于删除事件，我们只获取基本信息
	if dep, ok := obj.(*appsv1.Deployment); ok {
		log.Printf("[DELETE] 检测到 Deployment 删除: %s/%s\n", dep.Namespace, dep.Name)
		// todo 这里注释掉，仅作为扩展项执行脚本钩子
		//h.executeScript(dep.Name, dep.Namespace, "delete")
	} else {
		// 如果是 DeletedFinalStateUnknown 对象
		log.Printf("[DELETE] 检测到 Deployment 删除 (未知状态)")
	}
}

func (h *DeploymentHandler) handle(dep *appsv1.Deployment, eventType string) {
	log.Printf("========= 开始 [ADD Handler] %v 开始执行处理逻辑", time.Now().Format("2006-01-02 15:04:05"))
	// fixme 执行用户指定的脚本 {这里做成可扩展项，默认无需执行脚本钩子}
	//h.executeScript(dep.Name, dep.Namespace, eventType)

	// 原有的处理逻辑
	ctx := context.Background()
	name := dep.Name

	// 获取 selector pods
	podList, err := h.client.CoreV1().Pods(Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + name,
	})
	if err != nil {
		log.Println("获取 pods 失败:", err)
		return
	}
	if len(podList.Items) == 0 {
		log.Printf("没有找到 Pod，跳过: %s\n", name)
		return
	}

	pod := podList.Items[0]
	nodeIP := pod.Status.HostIP

	// 查找/创建 service
	svc, err := h.client.CoreV1().Services(Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		// 创建 NodePort Service
		log.Printf("未找到 Service，创建中: %s\n", name)
		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Selector: map[string]string{
					"app": name,
				},
				Ports: []corev1.ServicePort{
					{
						Port:       8080,
						TargetPort: intstr.FromInt(8080),
					},
				},
			},
		}
		svc, err = h.client.CoreV1().Services(Namespace).Create(ctx, svc, metav1.CreateOptions{})
		if err != nil {
			log.Println("创建 Service 失败:", err)
			return
		}
	}

	nodePort := svc.Spec.Ports[0].NodePort

	log.Printf("更新环境变量: IP=%s Port=%d\n", nodeIP, nodePort)

	// patch deploy
	patch := []byte(`
{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "name": "` + dep.Spec.Template.Spec.Containers[0].Name + `",
            "env": [
              { "name": "SPRING_CLOUD_NACOS_DISCOVERY_IP", "value": "` + nodeIP + `" },
              { "name": "SPRING_CLOUD_NACOS_DISCOVERY_PORT", "value": "` + fmt.Sprintf("%d", nodePort) + `" }
            ]
          }
        ]
      }
    }
  }
}`)

	_, err = h.client.AppsV1().Deployments(Namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
	)
	if err != nil {
		log.Println("Patch 失败:", err)
	}

	log.Printf("========= 完成 [ADD Handler] %v 完成执行处理逻辑", time.Now().Format("2006-01-02 15:04:05"))
	log.Println()
}

// executeScript 执行用户指定的脚本
func (h *DeploymentHandler) executeScript(deploymentName, namespace, eventType string) {
	// 根据操作系统选择合适的脚本
	var scriptPath string
	if runtime.GOOS == "windows" {
		scriptPath = "example-script.bat"
	} else {
		scriptPath = "./example-script.sh"
	}

	// 检查脚本文件是否存在
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		log.Printf("脚本文件不存在: %s", scriptPath)
		return
	}

	// 设置脚本可执行权限（非Windows系统）
	if runtime.GOOS != "windows" {
		if err := os.Chmod(scriptPath, 0755); err != nil {
			log.Printf("设置脚本权限失败: %v", err)
		}
	}

	// 执行脚本
	cmd := exec.Command(scriptPath, deploymentName, namespace, eventType)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("执行脚本失败 (%s): %v, 输出: %s", eventType, err, string(output))
	} else {
		log.Printf("脚本执行成功 (%s): %s", eventType, strings.TrimSpace(string(output)))
	}
}

// 确保 DeploymentHandler 实现了 cache.ResourceEventHandler 接口
var _ cache.ResourceEventHandler = &DeploymentHandler{}
