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
