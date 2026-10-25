<div align="center">

<img src="docs/icon.png" width="128" alt="Shoes 图标">

# Shoes

一个顺手的 Windows 代理客户端

给 [shoes](https://github.com/cfal/shoes) 内核做的原生图形界面：订阅一键更新、v2rayN 同款测速、规则分流、托盘常驻。

![Windows 10 / 11](https://img.shields.io/badge/Windows-10%20%2F%2011-23272e?style=flat-square&logo=windows&logoColor=white) ![Go 1.27](https://img.shields.io/badge/Go-1.27-23272e?style=flat-square&logo=go&logoColor=white) ![shoes 0.3.4](https://img.shields.io/badge/kernel-shoes%200.3.4-4f8a6c?style=flat-square) ![MyGo](https://img.shields.io/badge/UI-MyGo-6a717c?style=flat-square)

<img src="docs/screenshots/hero.webp" width="860" alt="仪表盘，浅色和深色">

</div>

## 为什么用它

**打开就能用。** 内核随 Shoes 一起常驻，右上角「启动」只负责接管系统代理。没点启动也能测速，也能把 `127.0.0.1:10808` 直接填给别的程序。

**订阅是活的。** 订阅地址绑在分组上，点「更新订阅」时同名节点就地换成新配置，新出的补上，下架的删掉；你手动加进这一组的节点不会被碰。读订阅时报的是 v2rayN 的 User-Agent，机场会回分享链接，而不是一整份 Clash YAML。

**测速和 v2rayN 一样准。** 「真连接延迟」另起一个只用来测速的内核，经每个节点连发两次 `generate_204` 取较小的那次；也有 TCPing。可以按延迟排序，一键「用最快的」；结果跟着节点配置走，换了配置或更新了订阅就清零重测。

**状态不骗人。** 网速曲线、本次流量、活动连接、内核内存、真实出口 IP 都在仪表盘上；没接管系统代理时，出口卡会直接告诉你浏览器现在是直连。

**安静地待在托盘。** 关窗口只是收进托盘，右键能启动停止、切节点、切模式；图标灰色是没接管，绿色是接管中。

**日志说人话。** 常见的单条连接报错翻成中文、一分钟内合并计数，默认只看重要的，报错标红，能搜、能选中复制、能一键复制给别人看。

**数据留在本机。** 节点和设置用 Windows DPAPI 加密，存在 `%APPDATA%\ShoesGUI`；改系统代理前先记一份恢复日志，就算程序意外退出，下次打开也会把原来的代理设置还回去。

## 截图

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/nodes-light.webp" alt="代理页"></td>
    <td width="50%"><img src="docs/screenshots/import-light.webp" alt="粘贴导入"></td>
  </tr>
  <tr>
    <td align="center">代理：分组、订阅更新、按延迟排序</td>
    <td align="center">粘贴导入：先预览，点了才导</td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/settings-dark.webp" alt="设置"></td>
    <td width="50%"><img src="docs/screenshots/logs-dark.webp" alt="日志"></td>
  </tr>
  <tr>
    <td align="center">设置：改了就存，不用找保存按钮</td>
    <td align="center">日志：报错标红，连接异常默认收起</td>
  </tr>
</table>

## 快速开始

1. 把 `ShoesGUI.exe` 和 shoes 内核 `shoes.exe`（0.3.4，Windows x64）放进同一个文件夹，双击 `ShoesGUI.exe`。
2. 在「代理」页点剪贴板图标，或者直接按 `Ctrl+V`，粘贴订阅地址或节点链接。看一眼预览，点「导入」。
3. 点闪电图标测速，挑一个快的（或者点「用最快的」），回到「仪表盘」点「启动」。

更细的说明在 [`outputs/使用说明.md`](outputs/使用说明.md)。

## 支持什么

| 类别 | 支持 |
| --- | --- |
| 协议 | SOCKS5 · HTTP · Shadowsocks（AEAD / 2022）· Trojan · VLESS · VMess · AnyTLS |
| 传输与加密 | TCP · WebSocket · XHTTP · TLS · Reality · Vision · VLESS 原生加密 |
| 导入 | 单条或批量分享链接 · Base64 订阅内容 · HTTP(S) 订阅地址 |
| 出站模式 | 规则（局域网和国内直连）· 全局 · 直连，运行中随时切 |

暂不支持：整份 Clash YAML / JSON 配置、gRPC、SS 插件、WebSocket Early Data、UDP/TUN，以及内核还没有出站实现的 Hysteria2 / TUIC。遇到这些会明确提示，不会悄悄降级。

## 快捷键

| 按键 | 作用 |
| --- | --- |
| `Ctrl+F` | 搜索节点 |
| `Ctrl+N` | 手动添加节点 |
| `Ctrl+V` | 在代理页打开粘贴导入（输入框里照常是粘贴） |

## 常见问题

<details>
<summary>订阅在 v2rayN 里能用，导进来却是一堆奇怪的节点？</summary>

很多机场按 User-Agent 决定回什么格式，不认识的客户端就回 Clash YAML。Shoes 现在报的是 `v2rayN/7.13.8 ShoesGUI/0.1`，能拿到分享链接；如果收到的仍是整份配置，导入页会直接提示不支持，不会把里面的 DNS、规则网址当成节点。
</details>

<details>
<summary>显示「运行中」，网页却打不开？</summary>

「运行中」只代表系统代理已经交给 Shoes，不代表当前节点一定通。先在代理页测个速，再看仪表盘的出口 IP 卡和日志页的红字。
</details>

<details>
<summary>为什么没有 TUN 模式？</summary>

shoes 内核在 Windows 上还不支持 TUN，所以界面上干脆没放这个开关。需要全局接管的程序，可以手动把代理设成 `127.0.0.1:10808`（HTTP 和 SOCKS5 共用一个端口）。
</details>

<details>
<summary>关了窗口它还在跑？</summary>

对，关窗口只是收进托盘，第一次关的时候会弹通知提醒。要退出请右键托盘图标选「退出」，退出时会恢复原来的系统代理。
</details>

## 从源码构建

```sh
cd work
go test ./...
go vet ./...
go build -trimpath -ldflags="-H windowsgui -s -w" -o ../outputs/ShoesGUI.exe .
```

exe 的图标、版本信息和 manifest 在 `work/rsrc_windows_amd64.syso` 里，`go build` 会自动带上。图标母版是 [`docs/icon.svg`](docs/icon.svg)（托盘用的简化版是 `docs/icon-tray-*.svg`），改完导出成 `work/resources/` 下的 PNG，再重新生成 syso：

```sh
cd work
go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --icon resources/icon.png --manifest gui \
  --product-name Shoes --file-description Shoes --product-version 0.1.0 --file-version 0.1.0 --original-filename ShoesGUI.exe
```

```
work/              GUI 源码（Go + MyGo）
  resources/       应用图标、托盘图标、侧栏图标
  rules/           国内规则（geosite / geoip cn）
outputs/           使用说明、第三方许可证（exe 不进仓库）
docs/              图标母版和 README 截图
```

## 致谢

- [cfal/shoes](https://github.com/cfal/shoes)：代理内核
- [egoist/mygo](https://github.com/egoist/mygo)：原生 UI 框架
- [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat)：国内规则（GPL-3.0）
- MiSans：界面字体
- 交互参考了 [FlClash](https://github.com/chen08209/FlClash) 和 [v2rayN](https://github.com/2dust/v2rayN)

第三方许可证都在 [`outputs/licenses/`](outputs/licenses/)。
