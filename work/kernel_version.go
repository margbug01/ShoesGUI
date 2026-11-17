package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// parseKernelVersion 从 `shoes --version` 的输出（"shoes 0.3.4"）里取版本号，认不出来返回空
func parseKernelVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) >= 2 && fields[0] == "shoes" {
		return fields[1]
	}
	return ""
}

// kernelVersion 问内核自己是哪一版，问不到返回空
func kernelVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	configureKernel(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseKernelVersion(string(out))
}

// kernelVersionText 设置页「Shoes 内核」下面那行：版本号，还没问到就先不说。
// 路径变了才重新问一次（放后台，问到了再刷新界面）
func (a *application) kernelVersionText() string {
	if path := a.settings.Kernel; path != "" && path != a.kernelVerFor {
		a.kernelVerFor, a.kernelVer = path, ""
		go func() {
			v := kernelVersion(path)
			a.window.Update(func() {
				if a.kernelVerFor == path {
					a.kernelVer = v
				}
			})
		}()
	}
	if a.kernelVer == "" {
		return ""
	}
	return "内核版本 " + a.kernelVer + "。内核带着 Hysteria2 等补丁，随 MyShoes 的更新一起升级，上游的新版内核不能直接替换"
}
