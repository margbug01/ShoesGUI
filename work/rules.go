package main

import (
	"embed"
	"os"
	"path/filepath"
)

// 来自 MetaCubeX/meta-rules-dat（GPL-3.0），首次运行写到数据目录，已有的文件不覆盖，留给以后的在线更新
//
//go:embed rules/geosite-cn.mrs rules/geoip-cn.mrs
var ruleFiles embed.FS

func ensureRules(dir string) (string, error) {
	target := filepath.Join(dir, "rules")
	if err := os.MkdirAll(target, 0700); err != nil {
		return "", err
	}
	entries, err := ruleFiles.ReadDir("rules")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		path := filepath.Join(target, entry.Name())
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			continue
		}
		data, err := ruleFiles.ReadFile("rules/" + entry.Name())
		if err != nil {
			return "", err
		}
		if err := atomicFile(path, data); err != nil {
			return "", err
		}
	}
	return target, nil
}
