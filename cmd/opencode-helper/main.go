package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

//go:embed handler.sh
var handlerScript []byte

const appName = "LLMGatewayOpencodeHelper"
const urlScheme = "llm-gateway-opencode"

func main() {
	if len(os.Args) < 2 {
		if len(os.Args) > 0 {
			handleURL(os.Args[0])
		}
		showUsage()
		return
	}

	switch os.Args[1] {
	case "register":
		register()
	case "unregister":
		unregister()
	case "handle":
		if len(os.Args) < 3 {
			log.Fatal("usage: opencode-helper handle <url>")
		}
		handleURL(os.Args[2])
	default:
		showUsage()
	}
}

func showUsage() {
	fmt.Println("LLM Gateway OpenCode 配置助手")
	fmt.Println()
	fmt.Println("用法:")
	fmt.Println("  opencode-helper register   注册 URL 协议处理器")
	fmt.Println("  opencode-helper unregister 卸载 URL 协议处理器")
	fmt.Println("  opencode-helper handle <url>  处理配置 URL")
	fmt.Println()
	fmt.Println("注册后，浏览器点击 llm-gateway-opencode:// 链接即可自动配置 OpenCode")
}

func register() {
	switch runtime.GOOS {
	case "darwin":
		registerDarwin()
	default:
		log.Fatalf("当前平台 %s 暂不支持自动注册，请手动配置", runtime.GOOS)
	}
}

func unregister() {
	switch runtime.GOOS {
	case "darwin":
		unregisterDarwin()
	default:
		log.Fatalf("当前平台 %s 暂不支持", runtime.GOOS)
	}
}

func registerDarwin() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("无法获取用户目录: %v", err)
	}

	appDir := filepath.Join(home, "Applications", appName+".app")
	contentsDir := filepath.Join(appDir, "Contents")
	macOSDir := filepath.Join(contentsDir, "MacOS")
	resourcesDir := filepath.Join(contentsDir, "Resources")

	dirs := []string{contentsDir, macOSDir, resourcesDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			log.Fatalf("创建目录失败 %s: %v", d, err)
		}
	}

	handlerPath := filepath.Join(macOSDir, "handler")
	if err := os.WriteFile(handlerPath, handlerScript, 0755); err != nil {
		log.Fatalf("写入 handler 脚本失败: %v", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.llm-gateway.opencode-helper</string>
	<key>CFBundleName</key>
	<string>LLM Gateway OpenCode Helper</string>
	<key>CFBundleVersion</key>
	<string>1.0</string>
	<key>CFBundleExecutable</key>
	<string>handler</string>
	<key>CFBundleURLTypes</key>
	<array>
		<dict>
			<key>CFBundleURLName</key>
			<string>LLM Gateway OpenCode Config URL</string>
			<key>CFBundleURLSchemes</key>
			<array>
				<string>%s</string>
			</array>
		</dict>
	</array>
</dict>
</plist>`, urlScheme)

	plistPath := filepath.Join(contentsDir, "Info.plist")
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		log.Fatalf("写入 Info.plist 失败: %v", err)
	}

	registerApp := exec.Command("open", appDir)
	_ = registerApp.Run()

	lsregister := exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister",
		"-f", appDir)
	_ = lsregister.Run()

	fmt.Printf("✅ 协议注册成功！\n")
	fmt.Printf("   App 路径: %s\n", appDir)
	fmt.Printf("   URL Scheme: %s://\n", urlScheme)
	fmt.Printf("   现在可以通过浏览器点击 %s:// 链接配置 OpenCode\n", urlScheme)
}

func unregisterDarwin() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("无法获取用户目录: %v", err)
	}

	appDir := filepath.Join(home, "Applications", appName+".app")
	if err := os.RemoveAll(appDir); err != nil {
		log.Fatalf("卸载失败: %v", err)
	}

	lsregister := exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister",
		"-u", appDir)
	_ = lsregister.Run()

	fmt.Println("✅ 协议已卸载")
}

func handleURL(url string) {
	fmt.Printf("handling url: %s\n", url)
}