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
