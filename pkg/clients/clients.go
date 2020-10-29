package clients

import (
	"context"
	"flag"
	"os"
	"time"

	ocpv1 "github.com/openshift/api/config/v1"
	cfgclientset "github.com/openshift/client-go/config/clientset/versioned"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type Interface interface {
	GetInfrastructure() (*ocpv1.Infrastructure, error)
	GetConfigMap(namespace, name string) (*v1.ConfigMap, error)
	GetSecret(namespace, name string) (*v1.Secret, error)
	ListNodes() ([]v1.Node, error)
	ListStorageClasses() ([]storagev1.StorageClass, error)
	ListPVs() ([]v1.PersistentVolume, error)
}

type clients struct {
	// Kubernetes API client
	KubeClient kubernetes.Interface

	// config.openshift.io client
	ConfigClient cfgclientset.Interface
}

var _ Interface = &clients{}

var (
	Timeout = flag.Duration("kubernetes-timeout", 10*time.Second, "Timeout of all Kubernetes calls")
)

func Create() (Interface, error) {
	var kubeconfig string

	// get the KUBECONFIG from env if specified (useful for local/debug cluster)
	kubeconfigEnv := os.Getenv("KUBECONFIG")

	if kubeconfigEnv != "" {
		klog.V(2).Infof("Using KUBECONFIG environment variable to connect")
		kubeconfig = kubeconfigEnv
	}

	var config *rest.Config
	var err error
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		klog.Infof("Building kube configs for running in cluster...")
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	cfgClient, err := cfgclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &clients{
		KubeClient:   kubeClient,
		ConfigClient: cfgClient,
	}, nil
}

func (c *clients) GetInfrastructure() (*ocpv1.Infrastructure, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *Timeout)
	defer cancel()
	return c.ConfigClient.ConfigV1().Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
}

func (c *clients) GetConfigMap(namespace, name string) (*v1.ConfigMap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *Timeout)
	defer cancel()
	return c.KubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *clients) GetSecret(namespace, name string) (*v1.Secret, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *Timeout)
	defer cancel()
	return c.KubeClient.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *clients) ListNodes() ([]v1.Node, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *Timeout)
	defer cancel()
	list, err := c.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *clients) ListStorageClasses() ([]storagev1.StorageClass, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *Timeout)
	defer cancel()
	list, err := c.KubeClient.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *clients) ListPVs() ([]v1.PersistentVolume, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *Timeout)
	defer cancel()
	list, err := c.KubeClient.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}
