/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"fmt"
	"github.com/polarismesh/polaris-controller/cmd/polaris-controller/app"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

var ignoredNamespaces = []string{
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
	"istio-system",
	"polaris-system",
	"kube-node-lease",
}

const (
	DefaultWeight = 100
)

// WaitForAPIServer waits for the API Server's /healthz endpoint to report "ok" with timeout.
func WaitForAPIServer(client clientset.Interface, timeout time.Duration) error {
	var lastErr error

	err := wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		healthStatus := 0
		result := client.Discovery().RESTClient().Get().AbsPath("/healthz").Do().StatusCode(&healthStatus)
		if result.Error() != nil {
			lastErr = fmt.Errorf("failed to get apiserver /healthz status: %v", result.Error())
			return false, nil
		}
		if healthStatus != http.StatusOK {
			content, _ := result.Raw()
			lastErr = fmt.Errorf("APIServer isn't healthy: %v", string(content))
			klog.Warningf("APIServer isn't healthy yet: %v. Waiting a little while.", string(content))
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %v", err, lastErr)
	}

	return nil
}

// CompareServiceAnnotationsChange 比较service变化
func CompareServiceAnnotationsChange(old, new map[string]string) ServiceChangeType {

	// 以下变更,需要同步对应的Service实例信息
	if old[PolarisHeartBeatTTL] != new[PolarisHeartBeatTTL] {
		return ServiceTTLChanged
	}
	if old[PolarisCustomWeight] != new[PolarisCustomWeight] {
		return ServiceCustomWeightChanged
	}
	if old[PolarisMetadata] != new[PolarisMetadata] {
		return ServiceMetadataChanged
	}

	// 以下变更不会引发Service同步
	if old[PolarisWeight] != new[PolarisWeight] {
		return ServiceWeightChanged
	}
	if old[PolarisEnableRegister] != new[PolarisEnableRegister] {
		return ServiceEnableRegisterChanged
	}
	return ""
}

// CompareServiceChange 判断本次更新是什么类型的
func CompareServiceChange(old, new *v1.Service) ServiceChangeType {
	if !IsPolarisService(new) {
		return ServicePolarisDelete
	}
	return CompareServiceAnnotationsChange(old.GetAnnotations(), new.GetAnnotations())
}

// IfNeedCreateServiceAlias Determine whether to create a service alias
func IfNeedCreateServiceAlias(old, new *v1.Service) bool {
	if old.Annotations[PolarisAliasNamespace] != new.Annotations[PolarisAliasNamespace] ||
		old.Annotations[PolarisAliasService] != new.Annotations[PolarisAliasService] {
		if new.Annotations[PolarisAliasNamespace] == "" || new.Annotations[PolarisAliasService] == "" {
			return false
		}
		return true
	}
	return false
}

// 用于判断是是否满足创建PolarisService的要求字段，这块逻辑应该在webhook中也增加
func IsPolarisService(svc *v1.Service, namespace *v1.Namespace, syncMode string) bool {
	// 默认忽略某些命名空间
	for _, namespaces := range ignoredNamespaces {
		if svc.GetNamespace() == namespaces {
			return false
		}
	}

	// Port是否合法 不能不设置port
	if len(svc.Spec.Ports) < 1 {
		klog.V(10).Infof("Service %s/%s has no ports", svc.GetNamespace(), svc.GetName())
		return false
	}

	// 没有设置 selector，polaris controller 不处理
	if svc.Spec.Selector == nil {
		klog.V(10).Infof("Service %s/%s has no selectors", svc.GetNamespace(), svc.GetName())
		return false
	}

	if syncMode == app.SyncModeNamespace {
		if !IsNamespacesNeedSync(namespace) {
			return false
		}
	}

	return true
}

// IgnoreService 添加 service 时，忽略一些不需要处理的 service
func IgnoreService(svc *v1.Service, namespace *v1.Namespace, syncMode string) bool {
	// 默认忽略某些命名空间
	for _, namespaces := range ignoredNamespaces {
		if svc.GetNamespace() == namespaces {
			return false
		}
	}

	if syncMode == app.SyncModeNamespace {
		if !IsNamespacesNeedSync(namespace) {
			return false
		}
	}

	return true
}

// IgnoreEndpoint 忽略一些命名空间下的 endpoints
func IgnoreEndpoint(endpoint *v1.Endpoints) bool {
	// 默认忽略某些命名空间
	for _, namespaces := range ignoredNamespaces {
		if endpoint.GetNamespace() == namespaces {
			return false
		}
	}
	return true
}

// IgnoreNamespace 忽略一些命名空间
func IgnoreNamespace(namespace *v1.Namespace) bool {
	// 默认忽略某些命名空间
	for _, ns := range ignoredNamespaces {
		if namespace.GetName() == ns {
			return false
		}
	}
	return true
}

// GetWeightFromService 从 k8s service 中获取 weight，如果 service 中没设置，则取默认值
func GetWeightFromService(svc *v1.Service) int {
	weight, ok := svc.GetAnnotations()[PolarisWeight]
	if ok {
		if w, err := strconv.Atoi(weight); err != nil {
			klog.Error("error to convert weight ", err)
			return DefaultWeight
		} else {
			return w
		}
	}
	return DefaultWeight
}

func IsNamespacesNeedSync(namespace *v1.Namespace) bool {
	sync, ok := namespace.Annotations[PolarisSync]
	// 注解不存在或者不需要同步
	if !ok || sync != app.IsEnableSync {
		return false
	} else {
		return true
	}
}
