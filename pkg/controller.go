package pkg

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	Namespace = "syncplant-backend"
)

func Run(ctx context.Context) error {
	// 尝试使用集群内配置，如果失败则尝试使用 kubeconfig 文件
	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("无法加载集群内配置: %v，尝试使用 kubeconfig 文件", err)

		// 获取 kubeconfig 文件路径
		var kubeconfig string
		if kubeconfig = os.Getenv("KUBECONFIG"); kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}

		// 使用 kubeconfig 文件创建配置
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	// 只 watch 指定的 namespace
	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		time.Minute,
		informers.WithNamespace(Namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.Everything().String()
		}),
	)

	deployInformer := factory.Apps().V1().Deployments().Informer()

	_, err = deployInformer.AddEventHandler(newDeploymentHandler(clientset))
	if err != nil {
		return err
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	log.Printf("开始监听 %s 命名空间下的 Deployment 事件...", Namespace)
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	<-ctx.Done()
	return nil
}
