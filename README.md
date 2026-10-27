<div align="center">

<img src="docs/icon.png" width="128" alt="Shoes 图标">

# Shoes

一个顺手的 Windows 代理客户端

给 [shoes](https://github.com/cfal/shoes) 内核做的原生图形界面：订阅一键更新、v2rayN 同款测速、规则分流、托盘常驻。

![Windows 10 / 11](https://img.shields.io/badge/Windows-10%20%2F%2011-23272e?style=flat-square&logo=windows&logoColor=white) ![Go 1.27](https://img.shields.io/badge/Go-1.27-23272e?style=flat-square&logo=go&logoColor=white) ![shoes 0.3.4](https://img.shields.io/badge/kernel-shoes%200.3.4-4f8a6c?style=flat-square) ![MyGo](https://img.shields.io/badge/UI-MyGo-6a717c?style=flat-square) ![GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-4f8a6c?style=flat-square)

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
| 分享与导出 | 单节点二维码 / 链接 · 分组或全部节点 TXT · 独立分享订阅地址 |
| 出站模式 | 规则（局域网和国内直连）· 全局 · 直连，运行中随时切 |

原生 HY2 测试版：配合本仓库 `native/` 补丁构建的 shoes 内核，可导入 `hy2://` / `hysteria2://`，通过 QUIC 转发 TCP 应用流量，无辅助进程。支持密码、SNI、SHA256 证书固定、连接复用、真连接测速和分享。下载带宽默认 100 Mbps，可在节点编辑中调整，0 为自动；填写过高不保证提速。无指纹的跳过证书验证会被拒绝。上游原版 0.3.4 不支持此出站，不能只更新 GUI。

暂不支持：整份 Clash YAML / JSON 配置、gRPC、SS 插件、WebSocket Early Data、UDP 应用/TUN、TUIC 出站，以及 HY2 的 Salamander 混淆和端口跳跃。遇到这些会明确提示，不会悄悄降级。HY2 使用 UDP 传输，TCPing 会跳过它，请用真连接测速。

## 分享到手机或其他客户端

- **单节点**：在「代理」页右键节点或点 `⋯` →「分享节点」，可复制标准分享链接或保存二维码 PNG。手机在 Shadowrocket 内打开扫一扫，不是用微信扫码。
- **批量**：选一个分组后点「导出当前分组」，复制多条链接或保存 TXT；选「全部」导出所有节点，选「未分组」只导出未分组节点。搜索框不影响导出范围。文件是一行一个节点链接，不是 Clash 配置。
- **订阅**：有绑定订阅的分组旁单独提供「分享订阅」，二维码和复制按钮分享的是原订阅地址，导入后可以更新；节点导出只是当前配置快照。

二维码在本地生成，不上传凭据。内容过长时只提供复制链接和 TXT；无法完整表达连接参数的节点会明确拒绝导出，批量导出也不会悄悄漏掉节点。链接、TXT 和二维码均包含连接凭据；订阅地址还可能授权整个账号的节点与流量，只分享给可信的人。

使用标准 URI 格式，优先面向 Shadowrocket 的扫码/链接导入流程；目标客户端仍须支持对应协议、Reality、XHTTP 等扩展，目前未经过 Shadowrocket 真机验证，不保证所有版本和参数组合都兼容。

## 内存与渲染

默认使用 MyGo 的软件渲染，避免小工具常驻 GPU 后端的额外开销，界面样式保持不变。2026-10-26 本机隔离测试（820×620、80 个假节点、连续刷新节点延迟）中，工作集从 GPU 路径约 97 MiB 降至软件路径约 56 MiB；这是同口径对照，不等同于任务管理器的专用工作集，实际占用随系统、DPI 和节点数量变化。

取舍：该测试的持续更新帧耗时中位数由约 3 ms 增至 16 ms，软件渲染并不保证高 DPI、快速滚动时与 GPU 同样流畅。需要 GPU 加速时，在启动前设置进程环境变量 `MYGO_GPU=1`；应用尊重显式配置，不改系统环境变量。切换需彻底退出再启动。

开发复测（Git Bash，在 `work/`）：`SHOES_NATIVE_MEMORY=1 go test -v -count=1` 打开并自动关闭假数据测试窗口；加 `MYGO_GPU=1` 测 GPU，加 `MYGO_FRAME_STATS=all` 记录帧耗时。不会读取实际节点或接管系统代理。

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

## 许可证

Shoes 以 [GPL-3.0](LICENSE) 发布，和内置的国内规则数据是同一个许可：可以自由使用、修改和再发布，再发布时也要同样开源。随附的 shoes 内核、MyGo、MiSans 字体等第三方组件按各自的许可证分发，原文都在 [`outputs/licenses/`](outputs/licenses/)。
