package pkg

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	handlerTimeout      = 3 * time.Minute
	handlerPollInterval = 3 * time.Second
)

var managedEnvNames = map[string]struct{}{
	"SPRING_CLOUD_NACOS_DISCOVERY_IP":   {},
	"SPRING_CLOUD_NACOS_DISCOVERY_PORT": {},
}

type DeploymentHandler struct {
	client *kubernetes.Clientset
}

func newDeploymentHandler(client *kubernetes.Clientset) *DeploymentHandler {
	return &DeploymentHandler{client: client}
}

func (h *DeploymentHandler) OnAdd(obj interface{}, _ bool) {
	dep, ok := obj.(*appsv1.Deployment)
	if !ok {
		log.Printf("[ADD] ignore unexpected object: %T\n", obj)
		return
	}

	log.Printf("[ADD] detected Deployment: %s/%s\n", dep.Namespace, dep.Name)
	h.processDeploymentAsync(dep.DeepCopy(), "add")
}

func (h *DeploymentHandler) OnUpdate(oldObj, newObj interface{}) {
	oldDep, oldOK := oldObj.(*appsv1.Deployment)
	newDep, newOK := newObj.(*appsv1.Deployment)
	if !oldOK || !newOK {
		log.Printf("[UPDATE] ignore unexpected object: %T -> %T\n", oldObj, newObj)
		return
	}

	if !shouldHandleDeploymentUpdate(oldDep, newDep) {
		return
	}

	log.Printf(
		"[UPDATE] detected Deployment change: %s/%s (generation %d -> %d)\n",
		newDep.Namespace, newDep.Name, oldDep.Generation, newDep.Generation,
	)
	h.processDeploymentAsync(newDep.DeepCopy(), "update")
}

func (h *DeploymentHandler) OnDelete(obj interface{}) {
	if dep, ok := obj.(*appsv1.Deployment); ok {
		log.Printf("[DELETE] detected Deployment: %s/%s\n", dep.Namespace, dep.Name)
		return
	}

	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if dep, ok := tombstone.Obj.(*appsv1.Deployment); ok {
			log.Printf("[DELETE] detected Deployment from tombstone: %s/%s\n", dep.Namespace, dep.Name)
			return
		}
	}

	log.Printf("[DELETE] ignore unexpected object: %T\n", obj)
}

func (h *DeploymentHandler) processDeploymentAsync(dep *appsv1.Deployment, eventType string) {
	go func() {
		log.Printf(
			"wait for Deployment to be ready before handling: %s/%s, event=%s, timeout=%s\n",
			dep.Namespace, dep.Name, eventType, handlerTimeout,
		)

		readyDep, err := h.waitForDeploymentReady(dep.Namespace, dep.Name, dep.Generation, handlerTimeout)
		if err != nil {
			log.Printf(
				"skip handling because Deployment did not become ready in time: %s/%s, event=%s, err=%v\n",
				dep.Namespace, dep.Name, eventType, err,
			)
			return
		}

		h.handle(readyDep, eventType)
	}()
}

func (h *DeploymentHandler) handle(dep *appsv1.Deployment, eventType string) {
	log.Printf("========= start [%s handler] %s", eventType, time.Now().Format("2006-01-02 15:04:05"))

	ctx := context.Background()
	namespace := dep.Namespace
	name := dep.Name

	containerName, currentIP, currentPort, ok := getManagedEnvValues(dep)
	if !ok {
		log.Printf("Deployment has no container to patch: %s/%s\n", namespace, name)
		return
	}

	labelSelector, err := buildDeploymentLabelSelector(dep)
	if err != nil {
		log.Printf("failed to build pod selector for Deployment %s/%s: %v\n", namespace, name, err)
		return
	}

	podList, err := h.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		log.Printf("failed to list pods for Deployment %s/%s: %v\n", namespace, name, err)
		return
	}
	if len(podList.Items) == 0 {
		log.Printf("no pods found for Deployment %s/%s\n", namespace, name)
		return
	}

	pod, ok := pickPodWithHostIP(podList.Items)
	if !ok {
		log.Printf("no pod with host IP found for Deployment %s/%s\n", namespace, name)
		return
	}

	svc, err := h.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			log.Printf("failed to get Service %s/%s: %v\n", namespace, name, err)
			return
		}

		log.Printf("Service not found, creating NodePort Service: %s/%s\n", namespace, name)
		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: serviceSelectorForDeployment(dep),
				Ports: []corev1.ServicePort{
					{
						Port:       8080,
						TargetPort: intstr.FromInt(8080),
					},
				},
			},
		}

		svc, err = h.client.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
		if err != nil {
			log.Printf("failed to create Service %s/%s: %v\n", namespace, name, err)
			return
		}
	}

	if svc.Spec.Type != corev1.ServiceTypeNodePort {
		log.Printf("Service is not NodePort, skip patch: %s/%s, type=%s\n", namespace, name, svc.Spec.Type)
		return
	}
	if len(svc.Spec.Ports) == 0 {
		log.Printf("Service has no ports, skip patch: %s/%s\n", namespace, name)
		return
	}

	nodeIP := pod.Status.HostIP
	nodePort := svc.Spec.Ports[0].NodePort
	if nodePort == 0 {
		log.Printf("Service has no allocated NodePort yet, skip patch: %s/%s\n", namespace, name)
		return
	}

	targetPort := fmt.Sprintf("%d", nodePort)
	log.Printf("resolved node address for %s/%s: IP=%s, Port=%s\n", namespace, name, nodeIP, targetPort)

	if currentIP == nodeIP && currentPort == targetPort {
		log.Printf("managed env is already up to date, skip patch: %s/%s\n", namespace, name)
		return
	}

	patch := []byte(`
{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "name": "` + containerName + `",
            "env": [
              { "name": "SPRING_CLOUD_NACOS_DISCOVERY_IP", "value": "` + nodeIP + `" },
              { "name": "SPRING_CLOUD_NACOS_DISCOVERY_PORT", "value": "` + targetPort + `" }
            ]
          }
        ]
      }
    }
  }
}`)

	if _, err = h.client.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
	); err != nil {
		log.Printf("failed to patch Deployment %s/%s: %v\n", namespace, name, err)
		return
	}

	log.Printf("========= finish [%s handler] %s", eventType, time.Now().Format("2006-01-02 15:04:05"))
	log.Println()
}

func (h *DeploymentHandler) executeScript(deploymentName, namespace, eventType string) {
	var scriptPath string
	if runtime.GOOS == "windows" {
		scriptPath = "example-script.bat"
	} else {
		scriptPath = "./example-script.sh"
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		log.Printf("script file does not exist: %s", scriptPath)
		return
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(scriptPath, 0755); err != nil {
			log.Printf("failed to chmod script: %v", err)
		}
	}

	cmd := exec.Command(scriptPath, deploymentName, namespace, eventType)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("failed to execute script (%s): %v, output: %s", eventType, err, string(output))
	} else {
		log.Printf("script executed successfully (%s): %s", eventType, strings.TrimSpace(string(output)))
	}
}

func shouldHandleDeploymentUpdate(oldDep, newDep *appsv1.Deployment) bool {
	if oldDep.ResourceVersion == newDep.ResourceVersion {
		return false
	}

	oldTemplate := sanitizePodTemplate(oldDep.Spec.Template)
	newTemplate := sanitizePodTemplate(newDep.Spec.Template)

	return !reflect.DeepEqual(oldTemplate, newTemplate)
}

func sanitizePodTemplate(template corev1.PodTemplateSpec) corev1.PodTemplateSpec {
	sanitized := template.DeepCopy()

	for i := range sanitized.Spec.InitContainers {
		sanitized.Spec.InitContainers[i].Env = filterManagedEnv(sanitized.Spec.InitContainers[i].Env)
	}
	for i := range sanitized.Spec.Containers {
		sanitized.Spec.Containers[i].Env = filterManagedEnv(sanitized.Spec.Containers[i].Env)
	}

	return *sanitized
}

func filterManagedEnv(envs []corev1.EnvVar) []corev1.EnvVar {
	if len(envs) == 0 {
		return envs
	}

	filtered := make([]corev1.EnvVar, 0, len(envs))
	for _, env := range envs {
		if _, managed := managedEnvNames[env.Name]; managed {
			continue
		}
		filtered = append(filtered, env)
	}

	return filtered
}

func (h *DeploymentHandler) waitForDeploymentReady(namespace, name string, targetGeneration int64, timeout time.Duration) (*appsv1.Deployment, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(handlerPollInterval)
	defer ticker.Stop()

	var lastErr error

	for {
		dep, err := h.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && deploymentReady(dep, targetGeneration) {
			return dep, nil
		}
		if err != nil {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("wait for Deployment ready timed out: %w", lastErr)
			}
			return nil, fmt.Errorf("wait for Deployment ready timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func deploymentReady(dep *appsv1.Deployment, targetGeneration int64) bool {
	if dep == nil || dep.Spec.Selector == nil {
		return false
	}
	if dep.Generation < targetGeneration {
		return false
	}
	if dep.Status.ObservedGeneration < targetGeneration {
		return false
	}

	var desiredReplicas int32 = 1
	if dep.Spec.Replicas != nil {
		desiredReplicas = *dep.Spec.Replicas
	}
	if desiredReplicas == 0 {
		return true
	}

	return dep.Status.UpdatedReplicas >= desiredReplicas &&
		dep.Status.ReadyReplicas >= desiredReplicas &&
		dep.Status.AvailableReplicas >= desiredReplicas
}

func buildDeploymentLabelSelector(dep *appsv1.Deployment) (string, error) {
	if dep.Spec.Selector == nil {
		return "", fmt.Errorf("deployment selector is nil")
	}

	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return "", err
	}

	return selector.String(), nil
}

func serviceSelectorForDeployment(dep *appsv1.Deployment) map[string]string {
	if dep.Spec.Selector != nil && len(dep.Spec.Selector.MatchLabels) > 0 {
		selector := make(map[string]string, len(dep.Spec.Selector.MatchLabels))
		for key, value := range dep.Spec.Selector.MatchLabels {
			selector[key] = value
		}
		return selector
	}

	return map[string]string{
		"app": dep.Name,
	}
}

func pickPodWithHostIP(pods []corev1.Pod) (*corev1.Pod, bool) {
	for i := range pods {
		if pods[i].Status.HostIP == "" {
			continue
		}
		return &pods[i], true
	}

	return nil, false
}

func getManagedEnvValues(dep *appsv1.Deployment) (containerName, ip, port string, ok bool) {
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return "", "", "", false
	}

	container := dep.Spec.Template.Spec.Containers[0]
	containerName = container.Name

	for _, env := range container.Env {
		switch env.Name {
		case "SPRING_CLOUD_NACOS_DISCOVERY_IP":
			ip = env.Value
		case "SPRING_CLOUD_NACOS_DISCOVERY_PORT":
			port = env.Value
		}
	}

	return containerName, ip, port, true
}

var _ cache.ResourceEventHandler = &DeploymentHandler{}
