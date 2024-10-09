# Kubernetes 存储管理

## 1. Dynamic Provisioner 例子与完整流程介绍

#### **原理概述**

在 Kubernetes 中，动态 provisioner 是一个实现了 `Provisioner` 接口的控制器，用于自动化存储卷的创建。当用户提交 PVC (PersistentVolumeClaim) 时，provisioner 根据定义的 StorageClass，自动创建相应的 PV (PersistentVolume)。这种自动化存储管理机制大大简化了卷的生命周期管理，减少了手动操作的复杂性。

#### **流程概述**
自定义动态 provisioner 的流程包括以下几个步骤：
1. 创建自定义的 Provisioner 逻辑，负责监听 PVC 的创建事件，并生成 PV。
2. 编写自定义的 StorageClass，使用该 Provisioner 动态创建卷。
3. 编写控制器代码，处理卷的创建与删除。
4. 部署自定义 provisioner 到 Kubernetes 集群中，并验证其功能。

#### **步骤 1: 自定义 Provisioner 代码实现**

https://github.com/ZhangSIming-blyq/custom-provisioner

首先，我们通过 Go 语言编写一个简单的自定义 provisioner，模拟卷的创建和删除过程。核心是自定义Provisioner结构体，实现Provision和Delete方法。Provision方法用于创建卷，Delete方法用于删除卷。

```go
package main

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"os"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v7/controller"
)

type customProvisioner struct {
	// Define any dependencies that your provisioner might need here, here I use the kubernetes client
	client kubernetes.Interface
}

// NewCustomProvisioner creates a new instance of the custom provisioner
func NewCustomProvisioner(client kubernetes.Interface) controller.Provisioner {
	// customProvisioner needs to implement "Provision" and "Delete" methods in order to satisfy the Provisioner interface
	return &customProvisioner{
		client: client,
	}
}

func (p *customProvisioner) Provision(options controller.ProvisionOptions) (*corev1.PersistentVolume, controller.ProvisioningState, error) {
	// Validate the PVC spec, 0 storage size is not allowed
	requestedStorage := options.PVC.Spec.Resources.Requests[corev1.ResourceStorage]
	if requestedStorage.IsZero() {
		return nil, controller.ProvisioningFinished, fmt.Errorf("requested storage size is zero")
	}

	// If no access mode is specified, return an error
	if len(options.PVC.Spec.AccessModes) == 0 {
		return nil, controller.ProvisioningFinished, fmt.Errorf("access mode is not specified")
	}

	// Generate a unique name for the volume using the PVC namespace and name
	volumeName := fmt.Sprintf("pv-%s-%s", options.PVC.Namespace, options.PVC.Name)

	// Check if the volume already exists
	volumePath := "/tmp/dynamic-volumes/" + volumeName
	if _, err := os.Stat(volumePath); !os.IsNotExist(err) {
		return nil, controller.ProvisioningFinished, fmt.Errorf("volume %s already exists at %s", volumeName, volumePath)
	}

	// Create the volume directory
	if err := os.MkdirAll(volumePath, 0755); err != nil {
		return nil, controller.ProvisioningFinished, fmt.Errorf("failed to create volume directory: %v", err)
	}

	// Based on the above checks, we can now create the PV, HostPath is used as the volume source
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: volumeName,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: options.PVC.Spec.Resources.Requests[corev1.ResourceStorage],
			},
			AccessModes:                   options.PVC.Spec.AccessModes,
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: volumePath,
				},
			},
		},
	}

	// Return the PV, ProvisioningFinished and nil error to indicate success
	klog.Infof("Successfully provisioned volume %s for PVC %s/%s", volumeName, options.PVC.Namespace, options.PVC.Name)
	return pv, controller.ProvisioningFinished, nil
}

func (p *customProvisioner) Delete(volume *corev1.PersistentVolume) error {
	// Validate whether the volume is a HostPath volume
	if volume.Spec.HostPath == nil {
		klog.Infof("Volume %s is not a HostPath volume, skipping deletion.", volume.Name)
		return nil
	}

	// Get the volume path
	volumePath := volume.Spec.HostPath.Path

	// Check if the volume path exists
	if _, err := os.Stat(volumePath); os.IsNotExist(err) {
		klog.Infof("Volume path %s does not exist, nothing to delete.", volumePath)
		return nil
	}

	// Delete the volume directory, using os.RemoveAll to delete the directory and its contents
	klog.Infof("Deleting volume %s at path %s", volume.Name, volumePath)
	if err := os.RemoveAll(volumePath); err != nil {
		klog.Errorf("Failed to delete volume %s at path %s: %v", volume.Name, volumePath, err)
		return err
	}

	klog.Infof("Successfully deleted volume %s at path %s", volume.Name, volumePath)
	return nil
}

func main() {
	// Use "InClusterConfig" to create a new clientset
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("Failed to create in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create clientset: %v", err)
	}

	provisioner := NewCustomProvisioner(clientset)

	// Important!! Create a new ProvisionController instance and run it; Once user creates a PVC, it would find the provisioner via storageClass's field "provisioner".
	pc := controller.NewProvisionController(clientset, "custom-provisioner", provisioner, controller.LeaderElection(false))
	klog.Infof("Starting custom provisioner...")
	pc.Run(context.Background())
}
```

#### **步骤 2: 部署自定义 Provisioner 到 Kubernetes**

**构建 Docker 镜像：**

首先，我们将上述代码打包成 Docker 镜像，以下是一个简单的 Dockerfile:

```Dockerfile
FROM golang:1.23 as builder
WORKDIR /workspace
COPY . .
WORKDIR /workspace/cmd/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o custom-provisioner .

FROM alpine:3.14
COPY --from=builder /workspace/cmd/custom-provisioner /custom-provisioner
ENTRYPOINT ["/custom-provisioner"]
```

**创建自定义 Provisioner 的 Deployment：**

这里因为我们要使用HostPath的tmp目录，所以需要在Deployment中挂载/tmp目录。

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-provisioner
spec:
  replicas: 1
  selector:
    matchLabels:
      app: custom-provisioner
  template:
    metadata:
      labels:
        app: custom-provisioner
    spec:
      containers:
        - name: custom-provisioner
          image: siming.net/sre/custom-provisioner:main_dc62f09_2024-09-29-010124
          imagePullPolicy: IfNotPresent
          env:
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: PROVISIONER_NAME
              value: custom-provisioner
          volumeMounts:
            - mountPath: /tmp
              name: tmp-dir
      volumes:
        - name: tmp-dir
          hostPath:
            path: /tmp
            type: Directory

```

#### **步骤 3: 创建 StorageClass**

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: custom-storage
provisioner: custom-provisioner
parameters:
  type: custom
```

#### **步骤 4: RBAC 权限配置**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-provisioner-role
rules:
  - apiGroups: [""]
    resources: ["persistentvolumes", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch", "create", "delete", "update"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]

---

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: custom-provisioner-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: custom-provisioner-role
subjects:
  - kind: ServiceAccount
    name: default
    namespace: system
```

#### **步骤 5: 创建 PVC 来触发 Provisioner**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: custom-pvc
spec:
  storageClassName: custom-storage
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
```

当 PVC 被创建时，Kubernetes 将触发自定义 provisioner 来创建并绑定卷。

```bash
# 成功部署custom-provisioner, 并且pod本地的tmp目录作为存储目录
kp
NAME                                  READY   STATUS    RESTARTS   AGE
controller-manager-578b69d9d4-t228b   1/1     Running   0          11d
custom-provisioner-58d77856f9-4m4l8   1/1     Running   0          3m19s

# 创建pvc后查看日志
k logs -f custom-provisioner-58d77856f9-4m4l8
I0928 17:25:36.663192       1 main.go:121] Starting custom provisioner...
I0928 17:25:36.663264       1 controller.go:810] Starting provisioner controller custom-provisioner_custom-provisioner-58d77856f9-4m4l8_6328a9e1-cdeb-4088-b8e2-c83b140429e3!
I0928 17:25:36.764192       1 controller.go:859] Started provisioner controller custom-provisioner_custom-provisioner-58d77856f9-4m4l8_6328a9e1-cdeb-4088-b8e2-c83b140429e3!
I0928 17:25:56.495556       1 controller.go:1413] delete "pv-system-custom-pvc": started
I0928 17:25:56.495588       1 main.go:90] Volume path /tmp/dynamic-volumes/pv-system-custom-pvc does not exist, nothing to delete.
I0928 17:25:56.495598       1 controller.go:1428] delete "pv-system-custom-pvc": volume deleted
I0928 17:25:56.502579       1 controller.go:1478] delete "pv-system-custom-pvc": persistentvolume deleted
I0928 17:25:56.502604       1 controller.go:1483] delete "pv-system-custom-pvc": succeeded
I0928 17:26:13.279654       1 controller.go:1279] provision "system/custom-pvc" class "custom-storage": started
I0928 17:26:13.279822       1 main.go:74] Successfully provisioned volume pv-system-custom-pvc for PVC system/custom-pvc
I0928 17:26:13.279839       1 controller.go:1384] provision "system/custom-pvc" class "custom-storage": volume "pv-system-custom-pvc" provisioned
I0928 17:26:13.279855       1 controller.go:1397] provision "system/custom-pvc" class "custom-storage": succeeded
I0928 17:26:13.279891       1 volume_store.go:212] Trying to save persistentvolume "pv-system-custom-pvc"
I0928 17:26:13.280969       1 event.go:377] Event(v1.ObjectReference{Kind:"PersistentVolumeClaim", Namespace:"system", Name:"custom-pvc", UID:"8dc69c67-609b-48aa-b5d7-ae932f91a8d7", APIVersion:"v1", ResourceVersion:"3337346", FieldPath:""}): type: 'Normal' reason: 'Provisioning' External provisioner is provisioning volume for claim "system/custom-pvc"
I0928 17:26:13.297182       1 volume_store.go:219] persistentvolume "pv-system-custom-pvc" saved
I0928 17:26:13.297444       1 event.go:377] Event(v1.ObjectReference{Kind:"PersistentVolumeClaim", Namespace:"system", Name:"custom-pvc", UID:"8dc69c67-609b-48aa-b5d7-ae932f91a8d7", APIVersion:"v1", ResourceVersion:"3337346", FieldPath:""}): type: 'Normal' reason: 'ProvisioningSucceeded' Successfully provisioned volume pv-system-custom-pvc

# 查看pvc
k get pvc 
NAME         STATUS   VOLUME                 CAPACITY   ACCESS MODES   STORAGECLASS     AGE
custom-pvc   Bound    pv-system-custom-pvc   1Gi        RWO            custom-storage   2m51s

# 已经动态绑定好pv
k get pv
NAME                   CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM               STORAGECLASS     REASON   AGE
pv-system-custom-pvc   1Gi        RWO            Delete           Bound    system/custom-pvc   custom-storage            2m53s

# 目录也成功创建
ls /tmp/dynamic-volumes/  
pv-system-custom-pvc
```

## 2. StorageClass 所有字段及其功能介绍

#### **StorageClass 示例 YAML**

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: custom-storage
provisioner: example.com/custom-provisioner
parameters:
  type: fast
  zone: us-east-1
  replication-type: none
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
mountOptions:
  - discard
  - nobarrier
```

#### **字段解释及控制逻辑**

##### **1. `provisioner`**

- **功能**:
    - `provisioner` 字段指定了用于动态创建卷的 provisioner。**<font color=seagreen>它决定了 Kubernetes 如何与外部存储系统交互</font>**。不同的 provisioner 可以对接不同类型的存储系统（如 AWS EBS、GCE Persistent Disks、自定义的 provisioner）。

- **受控组件**:
    - `kube-controller-manager` 中的 PersistentVolume controller 负责根据该字段选择合适的 provisioner 实现，并向其发出创建卷的请求。该字段的值会与定义在集群中的 provisioner 匹配，例如 `example.com/custom-provisioner`。

##### **2. `parameters`**

- **功能**:
    - `parameters` 字段用于向 provisioner 传递自定义参数。这些参数可以根据存储系统的特性进行定制。例如，`type` 参数可以定义存储类型（如高性能或标准存储），`zone` 参数可以定义存储卷的区域，`replication-type` 可以定义数据是否有复制策略。

- **受控组件**:
    - **<font color=seagreen>由 provisioner 自身解析并使用这些参数</font>**，在创建 PV 时根据传递的参数设置存储卷的属性。例如，如果 provisioner 是自定义的，它需要解释 `parameters` 中的内容，并与存储系统交互以执行卷的创建。

##### **3. `reclaimPolicy`**

- **功能**:
    - 定义了当 PVC 被删除时，PV 的行为。选项包括：
        - `Retain`: 卷不会被删除，数据保留。
        - `Delete`: 卷会被删除，存储资源也会被释放。
        - <strike>`Recycle`: 卷会被擦除并返回到未绑定状态（在 Kubernetes 1.9 之后已废弃）。</strike>

- **受控组件**:
    - 由 `kube-controller-manager` 中的 PersistentVolume controller 管理。**<font color=seagreen>当 PVC 释放后，controller 根据 `reclaimPolicy` 执行相应的删除或保留操作。</font>**

##### **4. `volumeBindingMode`**

- **功能**:
    - 决定了 PVC 何时绑定到 PV。选项包括：
        - `Immediate`: PVC 提交时立即绑定到可用的 PV。
        - `WaitForFirstConsumer`: 仅在 Pod 被调度时才绑定 PV。这可以避免资源分配的不平衡问题，特别适用于多可用区环境下存储和计算资源的协同调度。先调度后绑定这种延迟绑定方式在pv有多种选择的时候，先根据pod的需求选择pv，避免调度冲突(pv调度和pod调度冲突)

- **受控组件**:
    - **<font color=seagreen>`kube-scheduler` 负责在 `WaitForFirstConsumer` 模式下，根据 Pod 调度的节点选择合适的 PV</font>**，并执行 PVC 的绑定。在 `Immediate` 模式下，PVC 和 PV 的绑定则由 `kube-controller-manager` 中的 PersistentVolume controller 处理。

##### **5. `allowVolumeExpansion`**

- **功能**:
    - 当该字段设置为 `true` 时，允许用户动态扩展已经绑定的卷的大小。如果 PVC 需要更多存储空间，可以通过修改 PVC 的规格来触发卷扩展。

- **受控组件**:
    - `kube-controller-manager` 中的 `ExpandController` 负责处理卷扩展请求。当 PVC 被修改以请求更大的存储容量时，该 controller 会相应地对底层存储执行扩展操作，**<font color=seagreen>具体依赖于 storage provider 是否支持卷扩展</font>**。

##### **6. `mountOptions`**

- **功能**:
    - 定义卷在挂载时的选项。例如，在上述示例中，`discard` 选项表示在卷删除时自动丢弃数据，`nobarrier` 选项用于提高写入性能。这些选项会影响卷的使用方式。

- **受控组件**:
    - `kubelet` 负责在节点上处理挂载卷的操作。**<font color=seagreen>`kubelet` 在实际挂载卷到 Pod 时，会使用 StorageClass 中定义的挂载选项。</font>**

### **字段与 Controller 交互表格总结**

| 字段                | 作用                                      | 受控的组件                           |
|--------------------|-----------------------------------------|--------------------------------------|
| `provisioner`      | 指定动态卷 provisioner 使用哪个驱动         | `kube-controller-manager` 中的 PersistentVolume controller |
| `parameters`       | 向 provisioner 提供自定义参数                | 由 provisioner 自身逻辑解析           |
| `reclaimPolicy`    | 卷删除后保留、回收或删除                   | `kube-controller-manager` 中的 PersistentVolume controller |
| `volumeBindingMode`| PVC 何时绑定 PV                          | `kube-scheduler` (等待消费者模式) 或 `kube-controller-manager` (立即绑定模式) |
| `allowVolumeExpansion` | 允许扩展卷                             | `ExpandController` 在 `kube-controller-manager` 中处理 |
| `mountOptions`     | 卷挂载时的选项                             | `kubelet` 负责挂载时的处理            |

## 3. 整体挂载流程介绍：Attach、Detach、Mount、Unmount

Kubernetes 中的存储卷挂载流程涉及两个主要组件：**ControllerManager** 和 **Kubelet**。每个阶段都有不同的控制器负责管理卷的挂载、卸载等操作。

#### 1. **Attach（由 ControllerManager 处理）**
- **概述**: 当一个 Pod 被调度到某个节点，并且该 Pod 需要使用 PersistentVolume（如 EBS 或 GCE Persistent Disk），`AttachDetachController` 会将卷附加（Attach）到该节点。
- **过程**:
    1. Pod 被调度到某个节点。
    2. `AttachDetachController` 通过 Kubernetes API 获取 PVC 的相关信息，找到相应的 PersistentVolume。
    3. 使用 Cloud Provider 或者 CSI 驱动将卷附加到节点。(**<font color=tomato>调用csi的controller的controllerPublishVolume</font>**)
    4. 一旦卷附加成功，卷的状态将更新为 “Attached”，Pod 可以继续进入挂载阶段。

#### 2. **Detach（由 ControllerManager 处理）**
- **概述**: 当 Pod 被删除或调度到另一个节点时，`AttachDetachController` 会触发卷的卸载过程，将卷从原节点分离。
- **过程**:
    1. 当 Pod 终止时，`AttachDetachController` 检查卷是否仍然附加在节点上。
    2. 如果卷不再使用，控制器会通过 Cloud Provider 或 CSI 驱动将卷从节点上分离。
    3. 卷状态更新为 “Detached”，资源释放。

#### 3. **Mount（由 Kubelet 处理）**
- **概述**: 当卷附加到节点后，`kubelet` 会将卷挂载到 Pod 的容器中，这个过程通过 `VolumeManager` 来管理。
- **过程**:
    1. `kubelet` 监控到卷已附加，准备进行挂载操作。
    2. `VolumeManager` 负责将卷挂载到宿主机的文件系统（如 `/var/lib/kubelet/pods/...` 路径）。
    3. 卷挂载完成后，卷可通过容器内的目录访问。

#### 4. **Unmount（由 Kubelet 处理）**
- **概述**: 当 Pod 删除时，`kubelet` 会将该卷从宿主机文件系统中卸载。
- **过程**:
    1. `VolumeManager` 检查到卷不再使用，准备卸载。
    2. `kubelet` 执行卸载操作，卷从宿主机文件系统中移除。
    3. 卸载完成后，卷资源释放，Pod 生命周期结束。

### Pod 磁盘挂载具体流程：
1. 用户创建 Pod 并指定 PVC。
2. **Attach**: `AttachDetachController` 将卷附加到节点。
3. **Mount**: `kubelet` 将卷挂载到节点，并将其映射到 Pod 的容器中。
4. **Unmount**: 当 Pod 终止时，`kubelet` 执行卷的卸载操作。
5. **Detach**: `AttachDetachController` 将卷从节点分离。

要完整地理解 CSI（Container Storage Interface），我们可以通过编写一个简单的 CSI 驱动来演示其工作原理。这个例子将帮助你从基础层面理解 CSI 的各个组件：**CSI Identity**、**CSI Controller** 和 **CSI Node**。

## 4. CSI 驱动自主实现

https://github.com/ZhangSIming-blyq/hostpathcsi

### 组件介绍

![alt text](assets/storage/image.png)

一个 CSI 驱动包括三个主要部分：
- **<font color=tomato>CSI Identity</font>**：提供驱动信息，如名称、版本和功能。
- **<font color=tomato>CSI Controller</font>**：管理卷的生命周期（创建、删除、扩展等）。
- **<font color=tomato>CSI Node</font>**：负责将卷挂载到节点或 Pod 中。

kubernetes原生提供3个外部组件来与CSI驱动交互：
- **<font color=tomato>csi-attacher</font>**：负责将卷附加到节点。
- **<font color=tomato>csi-provisioner</font>**：负责创建和删除卷。
- **<font color=tomato>csi-driver-registrar</font>**：负责注册 CSI 驱动。

前两个要作为sidecar和csi-controller一起部署，后者作为sidecar和csi-node一起部署。都使用socket通信。

### 代码组织结构

```bash
.
├── LICENSE
├── Makefile
├── README.md
├── cmd
│   └── main.go
├── deploy
│   ├── Dockerfile
│   ├── csi-controller.yaml
│   ├── csi-node.yaml
│   ├── pvc.yaml
│   └── sc.yaml
├── go.mod
├── go.sum
└── pkg
    └── hostpathcsi
        ├── controller.go
        ├── identity.go
        └── node.go
```

### 1. **创建 PVC 并找到 CSI 插件处理逻辑**

当用户创建一个 PVC（Persistent Volume Claim）时，Kubernetes 会根据 PVC 所指定的 `StorageClass` 找到对应的 CSI 插件。

#### StorageClass 的作用：
- `StorageClass` 定义了如何动态创建存储卷。其关键字段是 `provisioner`，指定了由哪个 CSI 驱动来处理存储卷的创建。
- 例如，`provisioner` 的值为 `hostpath.csi.k8s.io`，Kubernetes 会根据这个名称找到已注册的 CSI 驱动，并通过它来完成存储卷的操作。

#### CSI 驱动注册：
- 在 Kubernetes 中，CSI 驱动通过 `CSIDriver` 对象进行注册。`CSIDriver` 对象保存了驱动的元数据信息，Kubernetes 通过它与 CSI 驱动进行交互。
- 例如，`csi-node-driver-registrar` 是一个负责在节点上注册 CSI 驱动的组件，它确保 Kubernetes 能识别并与节点上的 CSI 驱动通信。

当 PVC 创建后，`StorageClass` 会告诉 Kubernetes 使用哪个 CSI 驱动来处理该卷的创建逻辑。

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: custom-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: custom-csi-sc  # 使用之前定义的 StorageClass

---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: custom-csi-sc
provisioner: hostpath.csi.k8s.io  # 注意：这里的 provisioner 名字必须和你在 CSI 驱动中的名称一致
volumeBindingMode: Immediate       # 表示 PVC 立即绑定
reclaimPolicy: Delete              # PVC 删除时删除卷
```

### 2. 初始化grpc服务器并上面说的三个csi服务

```go
package main

import (
	"github.com/ZhangSIming-blyq/hostpathcsi/pkg/hostpathcsi"
	"log"
	"net"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
)

func main() {
	// 先删除已存在的 socket 文件，这是因为 kubelet 会在 /var/lib/kubelet/plugins/hostpath.csi.k8s.io/ 目录下创建一个 socket 文件
	// 先删除 socket 文件是为了确保新的进程可以绑定到同样的 socket 地址，避免因为旧的 socket 文件存在导致绑定失败或进程崩溃。
	// Unix Socket 适用于本地进程间通信，效率更高，安全性好，适用于 CSI 驱动和 Kubelet 的通信场景。
	// IP 地址（TCP/IP Socket） 适用于跨主机的进程通信，主要用于需要远程通信的场景。
	socket := "/var/lib/kubelet/plugins/hostpath.csi.k8s.io/csi.sock"
	if err := os.RemoveAll(socket); err != nil {
		log.Fatalf("failed to remove existing socket: %v", err)
	}

	listener, err := net.Listen("unix", socket)
	if err != nil {
		log.Fatalf("failed to listen on socket: %v", err)
	}

	server := grpc.NewServer()
	// 这里需要把三个服务注册到 gRPC 服务器上
	csi.RegisterIdentityServer(server, &hostpathcsi.IdentityServer{})
	csi.RegisterControllerServer(server, &hostpathcsi.ControllerServer{})
	csi.RegisterNodeServer(server, &hostpathcsi.NodeServer{})

	log.Println("Starting CSI driver...")
	// 启动 gRPC 服务器
	if err := server.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
```

identity是用于返回csi插件的详细信息，具备的能力等。

```go
// pkg/hostpathcsi/identity.go
package hostpathcsi

import (
	"context"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog"
)

// IdentityServer 注意因为要作为csi.ControllerServer的实现，所以需要实现csi.ControllerServer的所有方法
type IdentityServer struct {
	csi.UnimplementedIdentityServer
}

// GetPluginInfo 的作用是返回插件的信息，包括插件的名称和版本号
func (s *IdentityServer) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.Infof("Received GetPluginInfo request")

	return &csi.GetPluginInfoResponse{
		// csi要求插件的名称必顫是域名的逆序，这里使用了hostpath.csi.k8s.io
		Name:          "hostpath.csi.k8s.io",
		VendorVersion: "v1.0.0",
	}, nil
}

// GetPluginCapabilities 的作用是返回插件的能力，这里只返回了 ControllerService 的能力; 也就是说，这个插件只实现了 ControllerService
func (s *IdentityServer) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	// 什么是ControllerService能力呢？ControllerService是CSI规范中的一个服务，它负责管理卷的生命周期，包括创建、删除、扩容等操作
	klog.Infof("Received GetPluginCapabilities request")

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

func (s *IdentityServer) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	klog.Infof("Received Probe request")

	return &csi.ProbeResponse{}, nil
}
```

### 3. **`external-provisioner` 监听并调用 CSI 插件的 `CreateVolume` 和 `ControllerPublishVolume`**

#### `external-provisioner` 的作用：
- `external-provisioner` 是一个外部组件，它监听集群中 PVC 的创建请求。它会根据 PVC 中引用的 `StorageClass` 和对应的 `provisioner` 字段，找到相关的 CSI 驱动。
- 当 `external-provisioner` 发现 PVC 时，会向 CSI 控制器发出 `CreateVolume` 请求。

#### `CreateVolume` 调用：
- `CreateVolume` 方法是 CSI Controller 侧的一个接口，负责在存储系统中创建卷。这是 PVC 动态分配卷的关键步骤。
- `CreateVolume` 的实现通常会在底层存储系统中实际分配卷，并返回卷的 ID 和其他相关元数据信息给 Kubernetes。

#### `ControllerPublishVolume` 调用：
- 卷创建后，`ControllerPublishVolume` 负责将卷附加到指定的节点上。这通常是一个“模拟的”附加操作，特别是在 HostPath 这样的驱动中，它可能不涉及实际的物理附加，而是将卷关联到节点上。

#### PV 与 PVC 的绑定：
- Kubernetes 的 PV（PersistentVolume）控制器会自动将创建好的卷绑定到 PVC 上，完成 PVC 和 PV 的关联。
- `external-provisioner` 调用 `CreateVolume` 和 `ControllerPublishVolume` 成功后，Kubernetes 会创建 PV，并将其绑定到相应的 PVC，完成存储卷的动态分配。

```go
// pkg/hostpathcsi/controller.go
// Package hostpathcsi Description: 这个服务主要实现的是Volume管理流程中的"Provision阶段"和"Attach阶段"的功能。
package hostpathcsi

import (
	"context"
	"fmt"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog"
	"os"
)

// ControllerServer 用于实现 ControllerService
type ControllerServer struct {
	// 继承默认的 ControllerServer
	csi.ControllerServer
}

// CreateVolume 用于创建卷, 具体的创建"远程"真的数据卷出来
func (s *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.Infof("Received CreateVolume request for %s", req.Name)

	// 模拟 HostPath 卷的创建
	volumePath := "/tmp/csi/hostpath/" + req.Name
	if err := os.MkdirAll(volumePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create volume directory: %v", err)
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      req.Name,
			CapacityBytes: req.CapacityRange.RequiredBytes,
			VolumeContext: req.Parameters,
		},
	}, nil
}

// DeleteVolume 用于删除卷, 具体的删除"远程"真的数据卷
func (s *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.Infof("Received DeleteVolume request for %s", req.VolumeId)

	volumePath := "/tmp/csi/hostpath/" + req.VolumeId
	if err := os.RemoveAll(volumePath); err != nil {
		return nil, fmt.Errorf("failed to delete volume directory: %v", err)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume 用于发布卷, 这个是Attach阶段的功能
func (s *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// 在 HostPath 场景中，通常不需要 Controller 发布卷，因为它是本地存储
	return nil, fmt.Errorf("ControllerPublishVolume is not supported")
}

// ControllerUnpublishVolume 用于取消发布卷, 这个是Detach阶段的功能
func (s *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, fmt.Errorf("ControllerUnpublishVolume is not supported")
}

// ControllerGetCapabilities 返回 Controller 的功能
func (s *ControllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.Infof("Received ControllerGetCapabilities request")
	capabilities := []*csi.ControllerServiceCapability{
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				},
			},
		},
	}
	return &csi.ControllerGetCapabilitiesResponse{Capabilities: capabilities}, nil
}
```

### 4. **Pod 使用 PVC 触发 `NodePublishVolume` 挂载操作**

当一个 Pod 使用 PVC 时，Kubernetes 会调度该 Pod 到合适的节点，并触发 CSI 节点组件执行挂载操作。

#### `NodePublishVolume` 的作用：
- 当 Pod 被调度到节点并使用 PVC 时，`kubelet` 会与 `csi-node` 组件通信，触发 `NodePublishVolume` 操作。
- `NodePublishVolume` 负责将卷从存储系统挂载到节点的文件系统上，这样 Pod 就可以访问该存储卷。

#### 挂载流程：
1. Kubernetes 调度 Pod 到合适的节点，该节点的 `kubelet` 负责协调卷的挂载。
2. `kubelet` 会通过 CSI 驱动发出 `NodePublishVolume` 请求，要求将卷挂载到节点上的指定目录（如 `/var/lib/kubelet` 下的路径）。
3. `NodePublishVolume` 成功完成后，卷挂载到节点，Pod 可以使用该卷进行读写操作。

**重要提示**：
- 只有当 PVC 被 Pod 使用时，才会触发 `NodePublishVolume`。如果 PVC 没有被任何 Pod 使用，该方法不会被调用。

```go
// pkg/hostpathcsi/node.go
// Package hostpathcsi Description: 这个服务主要实现的是Volume管理流程中的"NodePublishVolume阶段"和"NodeUnpublishVolume阶段"的功能。
// 对应Mount和Unmount操作
package hostpathcsi

import (
	"context"
	"fmt"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog"
	"os"
	"path/filepath"
)

type NodeServer struct {
	csi.NodeServer
}

func (s *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.Infof("Received NodePublishVolume request for %s", req.VolumeId)

	targetPath := req.TargetPath
	sourcePath := "/tmp/csi/hostpath/" + req.VolumeId

	// 检查源路径是否存在
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("source path %s does not exist", sourcePath)
	}

	// 检查目标路径的父目录是否存在，若不存在则创建
	parentDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directory %s: %v", parentDir, err)
	}

	// 检查目标路径是否存在
	if fi, err := os.Lstat(targetPath); err == nil {
		// 如果目标路径已经是符号链接，检查它是否指向正确的源路径
		if fi.Mode()&os.ModeSymlink != 0 {
			existingSource, err := os.Readlink(targetPath)
			if err == nil && existingSource == sourcePath {
				klog.Infof("Target path %s already linked to correct source %s, skipping creation.", targetPath, sourcePath)
				return &csi.NodePublishVolumeResponse{}, nil
			}
			klog.Infof("Target path %s is a symlink but points to %s, removing it.", targetPath, existingSource)
		} else {
			klog.Infof("Target path %s exists but is not a symlink, removing it.", targetPath)
		}
		// 删除现有的文件或目录，避免冲突
		if err := os.RemoveAll(targetPath); err != nil {
			return nil, fmt.Errorf("failed to remove existing target path %s: %v", targetPath, err)
		}
	}

	// 创建软链接
	if err := os.Symlink(sourcePath, targetPath); err != nil {
		return nil, fmt.Errorf("failed to create symlink from %s to %s: %v", sourcePath, targetPath, err)
	}

	klog.Infof("Volume %s successfully mounted to %s", sourcePath, targetPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.Infof("Received NodeUnpublishVolume request for %s", req.VolumeId)

	targetPath := req.TargetPath

	// 检查目标路径是否存在且是软链接
	if fi, err := os.Lstat(targetPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			klog.Infof("Target path %s is a symlink, removing it.", targetPath)
			if err := os.RemoveAll(targetPath); err != nil {
				return nil, fmt.Errorf("failed to remove symlink at target path %s: %v", targetPath, err)
			}
			klog.Infof("Successfully removed symlink at %s", targetPath)
		} else {
			klog.Infof("Target path %s is not a symlink, skipping removal.", targetPath)
		}
	} else if os.IsNotExist(err) {
		klog.Infof("Target path %s does not exist, skipping unpublish.", targetPath)
	} else {
		return nil, fmt.Errorf("error checking target path %s: %v", targetPath, err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *NodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.Infof("Received NodeGetInfo request")

	// 获取node的主机名
	nodeID := "node1"

	// 可选：假如你支持Topologies，可以添加相关信息
	topology := &csi.Topology{
		Segments: map[string]string{
			"topology.hostpath.csi/node": nodeID,
		},
	}

	return &csi.NodeGetInfoResponse{
		NodeId:             nodeID,   // 返回节点ID
		AccessibleTopology: topology, // 返回可访问拓扑信息
	}, nil
}

// NodeGetCapabilities 返回该节点的能力信息
func (s *NodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.Infof("Received NodeGetCapabilities request")

	// 返回节点的能力信息，不包含 STAGE_UNSTAGE_VOLUME，表示跳过这个阶段
	capabilities := []*csi.NodeServiceCapability{
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					// 不包含 STAGE_UNSTAGE_VOLUME，跳过该能力
					Type: csi.NodeServiceCapability_RPC_UNKNOWN, // 表示无特定能力
				},
			},
		},
	}

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: capabilities,
	}, nil
}

// NodeStageVolume 空实现，用于跳过该操作
func (s *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.Infof("Received NodeStageVolume request but this operation is not needed, skipping.")
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume 空实现，用于跳过该操作
func (s *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.Infof("Received NodeUnstageVolume request but this operation is not needed, skipping.")
	return &csi.NodeUnstageVolumeResponse{}, nil
}
```

### 5. **部署文件**

我们使用statefulset来保证csi-controller拓扑状态的稳定性，因为他严格按照顺序更新pod，只有前一个pod停止并且删除后才会创建启动下一个pod；同时要做好RBAC。

对于csi-node，因为要和本地的kubelet交互，我们使用daemonset来保证每个节点都有一个pod。通信使用socket，要挂载到/var/lib/kubelet/plugins/hostpath.csi.k8s.io/目录下。对于容器内完成挂载链接的操作要设置mountPropagation: Bidirectional来保证双边的可见性。

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: csi-controller
  namespace: kube-system
spec:
  serviceName: "csi-controller"
  replicas: 1
  selector:
    matchLabels:
      app: csi-controller
  template:
    metadata:
      labels:
        app: csi-controller
    spec:
      serviceAccountName: csi-controller-sa
      containers:
        - name: csi-controller
          securityContext:
            privileged: true
          image: siming.net/sre/custom-csi:main_de5c0f9_2024-10-08-231124
          imagePullPolicy: IfNotPresent
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/kubelet/plugins/hostpath.csi.k8s.io/
              mountPropagation: Bidirectional
            - name: pods-dir  # 挂载 /var/lib/kubelet/pods 目录
              mountPath: /var/lib/kubelet/pods
              mountPropagation: Bidirectional
            - name: tmp-dir  # 挂载 /tmp 目录
              mountPath: /tmp
              mountPropagation: Bidirectional

        - name: external-provisioner
          image: quay.io/k8scsi/csi-provisioner:v2.0.0
          args:
            - "--csi-address=/csi/csi.sock"
            - "--leader-election=true"
          volumeMounts:
            - name: socket-dir
              mountPath: /csi

        - name: external-attacher
          image: quay.io/k8scsi/csi-attacher:v3.0.0
          args:
            - "--csi-address=/csi/csi.sock"
            - "--leader-election=true"
          volumeMounts:
            - name: socket-dir
              mountPath: /csi

      volumes:
        - name: socket-dir
          hostPath:
            path: /var/lib/kubelet/plugins/hostpath.csi.k8s.io/
            type: DirectoryOrCreate
        - name: pods-dir  # 宿主机 /var/lib/kubelet/pods 目录挂载
          hostPath:
            path: /var/lib/kubelet/pods
            type: Directory
        - name: volumes-dir  # 宿主机 /var/lib/kubelet/volumes 目录挂载
          hostPath:
            path: /var/lib/kubelet/volumes
            type: Directory
        - name: tmp-dir  # 宿主机 /tmp 目录挂载
          hostPath:
            path: /tmp
            type: Directory

---

apiVersion: v1
kind: ServiceAccount
metadata:
  name: csi-controller-sa
  namespace: kube-system

---

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: csi-controller-role
rules:
  - apiGroups: [""]
    resources: ["persistentvolumeclaims", "persistentvolumes", "nodes"]
    verbs: ["get", "list", "watch", "update", "create", "delete"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses", "volumeattachments", "csinodes"]
    verbs: ["get", "list", "watch", "update", "create"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["volumeattachments/status"]  # 增加对 volumeattachments/status 的 patch 权限
    verbs: ["patch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "watch", "list", "update", "patch", "create"]

---

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: csi-controller-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: csi-controller-role
subjects:
  - kind: ServiceAccount
    name: csi-controller-sa
    namespace: kube-system

---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: csi-node
  namespace: kube-system
  labels:
    app: csi-node
spec:
  selector:
    matchLabels:
      app: csi-node
  template:
    metadata:
      labels:
        app: csi-node
    spec:
      serviceAccountName: csi-node-sa
      containers:
        - name: csi-node
          securityContext:
            privileged: true
          image: siming.net/sre/custom-csi:main_de5c0f9_2024-10-08-170548
          volumeMounts:
            - name: plugin-dir
              mountPath: /var/lib/kubelet/plugins/hostpath.csi.k8s.io/
              mountPropagation: Bidirectional
            - name: pods-mount-dir
              mountPath: /var/lib/kubelet/pods
              mountPropagation: HostToContainer
            - name: tmp-dir  # 挂载 /tmp 目录
              mountPath: /tmp
              mountPropagation: Bidirectional

        - name: csi-driver-registrar
          image: quay.io/k8scsi/csi-node-driver-registrar:v2.0.0
          securityContext:
            privileged: true
          args:
            - "--csi-address=/csi/csi.sock"
            - "--kubelet-registration-path=/var/lib/kubelet/plugins/hostpath.csi.k8s.io/csi.sock"
          volumeMounts:
            - name: plugin-dir
              mountPath: /csi
            - name: registration-dir
              mountPath: /registration

      volumes:
        - name: plugin-dir
          hostPath:
            path: /var/lib/kubelet/plugins/hostpath.csi.k8s.io/
            type: DirectoryOrCreate
        - name: pods-mount-dir
          hostPath:
            path: /var/lib/kubelet/pods
            type: DirectoryOrCreate
        - name: registration-dir
          hostPath:
            path: /var/lib/kubelet/plugins_registry/
            type: DirectoryOrCreate
        - name: tmp-dir  # 宿主机 /tmp 目录挂载
          hostPath:
            path: /tmp
            type: Directory
```

### 6. **卷的卸载和资源的销毁**

#### 卸载卷（`NodeUnpublishVolume`）：
- 当 Pod 被删除或停止时，`kubelet` 会请求卸载卷，触发 `NodeUnpublishVolume` 方法。
- `NodeUnpublishVolume` 负责从节点文件系统中卸载卷，并删除挂载路径上的符号链接或执行卸载命令。

#### 删除存储卷（`DeleteVolume`）：
- 当用户删除 PVC 后，Kubernetes 会调用 CSI 驱动的 `DeleteVolume` 方法，删除后端存储中的卷。
- `DeleteVolume` 的作用是释放底层存储资源并删除与该卷相关的元数据。
- `external-provisioner` 在监听到 PVC 删除后，会与 CSI Controller 交互，通过 `DeleteVolume` 完成存储卷的回收和删除操作。

#### 反向流程：
- 当 Pod 停止使用 PVC 时，Kubernetes 会逐步执行卷的卸载过程，通过 `NodeUnpublishVolume` 完成从节点的卸载。
- 当 PVC 被删除时，`external-provisioner` 调用 `DeleteVolume` 来释放存储资源。

### 7. 部署查看效果

```bash
# 模拟创建
k apply -f pvc.yaml
persistentvolumeclaim/custom-pvc created

k apply -f demo.yaml 
pod/my-pod-1 created

k get pvc
NAME         STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS    AGE
custom-pvc   Bound    pvc-dce4da52-28cf-4514-854e-545df5a31a29   1Gi        RWO            custom-csi-sc   5s

 sudo touch /tmp/csi/hostpath/pvc-dce4da52-28cf-4514-854e-545df5a31a29/testfile               

kp
NAME                                      READY   STATUS      RESTARTS   AGE
calico-kube-controllers-5b564d9b7-5lbrn   1/1     Running     0          33d
canal-lxk8w                               2/2     Running     0          33d
coredns-54cc789d79-mrbpb                  1/1     Running     0          33d
coredns-autoscaler-6ff6bf758-hrxmh        1/1     Running     0          33d
csi-controller-0                          3/3     Running     0          10h
csi-node-wmsq7                            2/2     Running     0          16h
metrics-server-657c74b5d8-jjxzd           1/1     Running     0          33d
my-pod-1                                  1/1     Running     0          17s
rke-coredns-addon-deploy-job-rmw2r        0/1     Completed   0          33d
rke-ingress-controller-deploy-job-2z4d6   0/1     Completed   0          33d
rke-metrics-addon-deploy-job-9bs7c        0/1     Completed   0          33d
rke-network-plugin-deploy-job-2pzvs       0/1     Completed   0          33d

k exec -it my-pod-1 ls /mnt/data   
kubectl exec [POD] [COMMAND] is DEPRECATED and will be removed in a future version. Use kubectl exec [POD] -- [COMMAND] instead.
testfile

# 模拟删除
kdel -f demo.yaml   
warning: Immediate deletion does not wait for confirmation that the running resource has been terminated. The resource may continue to run on the cluster indefinitely.
pod "my-pod-1" force deleted

kdel -f pvc.yaml  
warning: Immediate deletion does not wait for confirmation that the running resource has been terminated. The resource may continue to run on the cluster indefinitely.
persistentvolumeclaim "custom-pvc" force deleted

ls /tmp/csi/hostpath 

```

### 整体流程总结：

1. **PVC 创建**：
    - 用户创建 PVC 并指定 `StorageClass`。`StorageClass` 的 `provisioner` 字段指定了哪个 CSI 驱动负责处理卷。
    - `external-provisioner` 监听 PVC 事件，并通过 `provisioner` 字段找到相应的 CSI 插件。

2. **卷的创建和附加**：
    - `external-provisioner` 调用 `csi-controller` 的 `CreateVolume` 方法，创建存储卷。
    - `ControllerPublishVolume` 将卷附加到节点。

3. **Pod 使用 PVC 触发卷挂载**：
    - 当 Pod 使用 PVC 时，`kubelet` 通过 `NodePublishVolume` 请求将卷挂载到节点。
    - Pod 可以在挂载成功后使用该卷进行数据读写。

4. **卸载与删除**：
    - 当 Pod 停止或删除时，`NodeUnpublishVolume` 卸载卷。
    - 当 PVC 被删除时，`DeleteVolume` 删除卷，并释放底层存储资源。

这个过程描述了从 PVC 的创建、卷的动态分配、Pod 使用卷，再到卷的卸载和删除的完整生命周期。

## provisioner 和 csi的区别

| **分类**             | **Provisioner**                                                     | **CSI (Container Storage Interface)**                            |
|----------------------|---------------------------------------------------------------------|------------------------------------------------------------------|
| **定义**             | Provisioner 是负责动态或静态分配存储卷的组件。可以是基于 CSI 也可以不是。 | CSI 是一种标准接口，定义了 Kubernetes 如何与存储系统交互，通常用于动态卷管理。 |
| **工作机制**         | Provisioner 监听 PVC 事件，动态或静态创建 PV，绑定 PVC。可以是 CSI 驱动的 `external-provisioner`，也可以是传统非 CSI 的 Provisioner。 | CSI 通过标准接口与 Kubernetes 交互，提供卷的创建、挂载、卸载等功能，实际操作由存储插件实现。 |
| **通用性**           | Provisioner 可以是非 CSI 方案，例如基于传统存储系统的动态分配机制。也可以支持静态预创建的 PV。 | CSI 是专门为容器环境设计的通用存储接口，支持任何遵循 CSI 标准的存储系统。 |
| **工作方式**         | Provisioner 可以通过 StorageClass 中的 `provisioner` 字段指定，动态分配存储卷，不一定依赖 CSI，可以是特定存储厂商的原生方案。 | CSI 提供标准化接口，负责与 Kubernetes API 交互，执行卷的创建、挂载、卸载等，存储厂商通过实现 CSI 驱动来提供具体功能。 |
| **功能**             | Provisioner 负责创建、管理 PV 和 PVC，提供了动态卷分配的能力，支持通过插件或内置机制实现（例如 `kubernetes.io/aws-ebs`）。 | CSI 提供标准的 API 规范，允许 Kubernetes 与不同存储系统交互，完成卷管理。 |
| **外部组件**         | `external-provisioner` 是典型的 CSI-based Provisioner，也有非 CSI 的动态 Provisioner 通过特定的 API 与 Kubernetes 交互。 | CSI 是存储接口规范，具体的存储实现依赖不同的 CSI 驱动，负责处理存储的实际操作。 |
| **动态 vs 静态**     | 动态 Provisioner 动态创建卷，静态 Provisioner 允许预创建卷并手动分配给 PVC。 | CSI 一般用于动态存储卷分配，但也可以通过 PV 实现静态卷分配。 |
| **示例**             | 1. 动态：`kubernetes.io/aws-ebs`，`nfs-client` 动态创建 PV。<br>2. 静态：手动创建 PV，绑定 PVC。 | HostPath CSI、AWS EBS CSI、NFS CSI 等，可以通过 CSI 驱动创建和管理存储卷。 |
| **适用范围**         | 适用于传统存储系统和非容器化存储，支持 Kubernetes 的动态存储分配。 | 适用于容器化环境，支持各种存储类型，标准化接口确保跨平台和跨供应商的存储兼容。 |
| **主要职责**         | 1. 监听 PVC 创建请求<br>2. 调用 API 或存储系统接口创建 PV，绑定 PVC。 | 1. 提供跨存储供应商标准接口<br>2. 提供容器化环境中的存储卷管理操作。 |
| **核心接口/方法**    | - 动态：`Provision`<br>- 静态：手动创建 PV 并绑定 PVC | - `NodePublishVolume`<br>- `ControllerPublishVolume`<br>- `CreateVolume` |
| **优势**             | 动态 Provisioner 提供了非 CSI 环境下的动态存储卷管理。                 | CSI 通过标准化接口，支持广泛的存储系统和供应商，具有高度扩展性。 |
| **典型场景**         | 动态：AWS EBS、GCE PD 等云供应商存储卷，NFS 动态卷客户端。<br>静态：预分配卷，管理员手动操作。 | 适用于支持 CSI 的存储系统，HostPath、GlusterFS、AWS EBS 等支持 CSI 的存储。 |


**<font color=yellowgreen>CSI 需要与 Provisioner 结合使用，但 Provisioner 不一定需要依赖 CSI。</font>**