package pkg

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestShouldHandleDeploymentUpdateIgnoresManagedEnvOnlyChange(t *testing.T) {
	oldDep := newTestDeployment("demo", "default", "demo:v1", []corev1.EnvVar{
		{Name: "SPRING_CLOUD_NACOS_DISCOVERY_IP", Value: "10.0.0.1"},
		{Name: "SPRING_CLOUD_NACOS_DISCOVERY_PORT", Value: "30080"},
	})
	newDep := newTestDeployment("demo", "default", "demo:v1", []corev1.EnvVar{
		{Name: "SPRING_CLOUD_NACOS_DISCOVERY_IP", Value: "10.0.0.2"},
		{Name: "SPRING_CLOUD_NACOS_DISCOVERY_PORT", Value: "30081"},
	})

	oldDep.ResourceVersion = "1"
	newDep.ResourceVersion = "2"

	if shouldHandleDeploymentUpdate(oldDep, newDep) {
		t.Fatalf("expected managed env only change to be ignored")
	}
}

func TestShouldHandleDeploymentUpdateDetectsTemplateChange(t *testing.T) {
	oldDep := newTestDeployment("demo", "default", "demo:v1", nil)
	newDep := newTestDeployment("demo", "default", "demo:v2", nil)

	oldDep.ResourceVersion = "1"
	newDep.ResourceVersion = "2"

	if !shouldHandleDeploymentUpdate(oldDep, newDep) {
		t.Fatalf("expected image change to trigger update handling")
	}
}

func newTestDeployment(name, namespace, image string, env []corev1.EnvVar) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: image,
							Env:   env,
						},
					},
				},
			},
		},
	}
}
