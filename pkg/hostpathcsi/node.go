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
