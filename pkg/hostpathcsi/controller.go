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
