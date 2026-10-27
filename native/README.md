# 原生 Hysteria2 出站（测试版）

此实现运行在 shoes Rust 内核进程中，不启动 Hysteria 或 sing-box 辅助进程，也没有本机 SOCKS 桥接。GUI 原有本机 mixed 入站、分流、Clash API、测速与进程管理保持不变。

## 范围

- HTTP/3 密码认证、QUIC 双向流 TCP 转发、连接复用、超时和关闭后重建。
- 复用现有 TLS 信任链与 SHA256 证书固定实现。GUI 不接受无指纹跳过验证。
- 应用层 UDP、Salamander、端口跳跃和代理链非首跳暂不支持。这里的 TCP 转发依然通过 UDP/QUIC 承载。
- 上传采用 Quinn 自带 BBR 自适应拥塞控制，仅作用于 HY2；下载向服务端通告接收带宽（GUI 导入默认 100 Mbps，0 表示自动）。没有实现固定速率 Brutal 上传，也不声称与官方或 sing-box 的拥塞策略完全一致。
- GUI 检测到 HY2 节点时，设置 QUIC 连接接收窗口 16 MiB、单流窗口 8 MiB、发送窗口 4 MiB。仍经过内核统一资源预算；无 HY2 节点不修改原有窗口。较大的窗口允许更高吞吐，也意味着更高的潜在内存占用。

## 内核来源与构建

基线：`https://github.com/cfal/shoes.git`，提交 `fb1fa5b8bbc5326c86a02fb4b1f17b37ca41d018`（源码版本号 0.3.4）。这是固定源码基线，不保证等同于此前随包的预编译二进制。`hysteria2.patch` 包含完整修改和新文件。

Windows 需要 MSVC C++ 构建工具、CMake、Git 和 Rust 1.95.0。在本仓库根目录的 Git Bash 执行：

```sh
git clone -c core.autocrlf=false https://github.com/cfal/shoes.git native/build
git -C native/build checkout fb1fa5b8bbc5326c86a02fb4b1f17b37ca41d018
git -C native/build apply ../hysteria2.patch
cd native/build
cargo +1.95.0 test --locked hysteria2 --lib
cargo +1.95.0 build --locked --release --bin shoes
```

Windows 必须在新克隆中保留 LF（上面的 `-c core.autocrlf=false` 会写入新仓库配置）。本机全局 autocrlf=true 会使基线归档/检出变成 CRLF，直接应用 LF 补丁时报上下文不匹配；本轮已使用 LF 固定基线验证完整补丁应用和源码一致性，没有修改用户全局 Git 配置。

构建成功后，退出 Shoes，先备份 `outputs/shoes.exe`，再将 `native/build/target/release/shoes.exe` 放到该位置。不要只更新 GUI，否则原版内核会拒绝 `hysteria2` 配置。GUI 的编译方法见主 README。

macOS（Apple 芯片）需要 Xcode Command Line Tools、Git 和 Rust 1.95.0（`rustup toolchain install 1.95.0 --profile minimal`），不需要 CMake，也没有换行问题。在本仓库根目录执行上面同样的六条命令，产物是 `native/build/target/release/shoes`（Mach-O arm64）。把它复制到 `work/resources/darwin-arm64/shoes`，`mygo build` 会打进 `ShoesGUI.app/Contents/Resources/`。发版时 Windows 内核放在 `work/resources/windows-amd64/shoes.exe`，`mygo build` 会放到 `ShoesGUI.exe` 旁边，也会打进自动更新包。这两个位置都在 `.gitignore` 里，不进仓库。2026-10-27 在 macOS 上按此构建，`cargo +1.95.0 test --locked hysteria2 --lib` 14 项通过。

## 验证

GUI 单测：在 `work/` 执行 `go test ./...` 和 `go vet ./...`。HY2 测试覆盖 URI、证书固定、拒绝未支持参数、分享往返、测速配置和离屏编辑控件。

本轮最终内核验证：14 项 HY2 相关测试、23 项连接器测试、11 项双向转发测试通过。配置回归 214 项中 213 项通过；`test_recursive_certificate_embedding` 将 Windows 临时路径未经转义插入 YAML 双引号，报 `did not find expected hexadecimal number`，此测试失败未修改。未宣称完整内核测试全部通过。

可选真实服务器验证：用环境变量 `SHOES_HY2_TEST_URI` 提供含 `pinSHA256` 的测试节点链接，`SHOES_HY2_TEST_KERNEL` 提供内核绝对路径。不要将真实链接提交到仓库。

- `python native/live-hy2.py "$SHOES_HY2_TEST_KERNEL"`：8 个并发 HTTPS 请求、完整 10 MB 下载、错误密码和错误证书指纹；失败场景必须在内核日志中匹配对应拒绝原因，不能仅靠请求失败判定。通过测试专用 UDP 转发器丢弃数据 35 秒，确认断线请求失败、恢复后重新认证一次并成功请求；不重启远端 HY2，也不改防火墙或用户网络。结束断言内核进程退出、转发线程结束、临时配置和日志目录不存在。
- 在 `work/` 执行 `go test -count=1 -run TestHY2Live -v`：真实 GUI 测速入口、完整分流配置、退出后的临时文件清理；不读取用户已有配置，不接管系统代理。

2026-10-26 的一次本机测试：原生下载窗口调整前约 0.72 MB/s，调整后两轮约 3.94 / 4.84 MB/s，GUI 真连接延迟 84 ms。同期 sing-box 两轮约 5.60 / 9.48 MB/s。都是短时 10 MB 下载，不代表稳定带宽，不能据此声称原生实现与 sing-box 性能相同。

## 上传回归调查（2026-10-26）

原先的短时下载测试没有覆盖上传，不能作为双向性能验收。本轮使用独立本机监听端口，不接管系统代理、不终止用户的客户端。测试端点通过 SSH 临时运行于服务端回环地址，不修改 HY2 服务或系统网络配置；上传响应包含服务端实际读取的字节数与 SHA256，客户端下载完整响应后才停止计时。下载同样核对长度和 SHA256。

`bench-hy2.py` 从本机对照配置读取节点，证书通过 SSH 读取。密码不打印，临时配置结束后删除。v2rayN 运行的对照内核是 sing-box 1.13.4，上下行参数均为 100 Mbps。原配置启用了 insecure，本测试改为信任服务器实际证书，未关闭验证。Shoes 始终使用 SHA256 固定证书。每个方向每次 4,000,000 字节、单并发；每轮新建客户端，先上传再下载，顺序为 Shoes/sing-box、sing-box/Shoes、Shoes/sing-box。吞吐包含建连和完整响应时间，不等同于 Cloudflare 网页多连接峰值。

修复前（Mbps）：

| 轮次 | Shoes 上传 | sing-box 上传 | Shoes 下载 | sing-box 下载 |
| --- | ---: | ---: | ---: | ---: |
| 1 | 0.450 | 29.347 | 38.949 | 74.740 |
| 2 | 0.483 | 25.501 | 56.082 | 52.944 |
| 3 | 0.492 | 29.894 | 28.350 | 74.042 |
| 中位数 | 0.483 | 29.347 | 38.949 | 74.042 |

额外诊断：Cubic 上传 0.567 Mbps；结束时 RTT 76.16 ms、cwnd 4,671 字节、累计发送 3,093 包、丢失 260 包、239 次拥塞事件。DATA_BLOCKED、STREAM_DATA_BLOCKED 均为零。CPU 约占单核 0.06%，不是 CPU 饱和。把发送窗口从 4 MiB 放大到 32 MiB，上传仍为 0.504 Mbps。该路径的首要瓶颈是丢包触发的 Cubic 收缩，而不是配置的发送窗口。此诊断版本的流关闭日志存在异步落盘延迟，第一次上传的统计记录落在后一个下载结果下；新版脚本按完成流数量等待并切片，不把它误认为下载统计。

`QuicStream::poll_write` 直接传递 Quinn 的写入结果与唤醒，转发器保留未写出的缓存并在 Pending 时让出；没有看到吞掉部分写入或忙轮询。HTTP/3 认证完成后 TCP 数据走独立 QUIC 双向流，不经过 HTTP/3 请求体调度。此次先只对 HY2 选择 Quinn 自带 BBR，其他 QUIC 协议保持原算法；接收和发送窗口不再增大。诊断通过 `SHOES_HY2_DIAGNOSTICS=1` 开启，只记录非敏感数值，不记录认证头。

修复后同口径 4 MB（Mbps，`outputs/hy2-after.json`）：

| 轮次 | Shoes 上传 | sing-box 上传 | Shoes 下载 | sing-box 下载 |
| --- | ---: | ---: | ---: | ---: |
| 1 | 39.107 | 29.570 | 34.485 | 39.199 |
| 2 | 13.813 | 38.850 | 24.828 | 64.606 |
| 3 | 13.445 | 38.714 | 39.166 | 54.913 |
| 中位数 | 13.813 | 38.714 | 34.485 | 54.913 |

上传比原生修复前中位数提高 28.6 倍，但只有同期对照的 35.7%。显式设置的 `--min-upload-ratio 0.5` 在这组测试中**失败**，未删除该失败记录，也不将下列长传输结果替代同口径验收。短下载也没有追平对照，未宣称全场景达标。

补充 32 MB 单并发长传输（Mbps，`outputs/hy2-long.json`；这是另一组条件，不与修复前的 4 MB 作倍数比较）：

| 轮次 | Shoes 上传 | sing-box 上传 | Shoes 下载 | sing-box 下载 |
| --- | ---: | ---: | ---: | ---: |
| 1 | 69.595 | 71.042 | 87.507 | 36.207 |
| 2 | 53.781 | 39.535 | 81.829 | 81.403 |
| 3 | 43.334 | 69.604 | 77.276 | 85.158 |
| 中位数 | 53.781 | 69.604 | 81.829 | 81.403 |

补充 4 路并发，每路 4 MB，表中为总吞吐（Mbps，`outputs/hy2-parallel.json`）：

| 轮次 | Shoes 上传 | sing-box 上传 | Shoes 下载 | sing-box 下载 |
| --- | ---: | ---: | ---: | ---: |
| 1 | 45.984 | 58.969 | 77.037 | 76.954 |
| 2 | 57.847 | 63.788 | 84.668 | 82.231 |
| 3 | 78.895 | 67.144 | 84.066 | 84.115 |
| 中位数 | 57.847 | 63.788 | 84.066 | 82.231 |

带宽协商与剩余差距：实际服务器未配置 `bandwidth`、`ignoreClientBandwidth` 或 `quic` 覆盖项；下载使用通告的 100 Mbps。对照版本的 [sing-quic v0.6.0 客户端](https://github.com/SagerNet/sing-quic/blob/v0.6.0/hysteria2/client.go) 在服务端未要求自动带宽且 `up_mbps` 非零时选 Brutal，否则选 BBR。Shoes 选择的是 Quinn 的自适应 BBR，没有移植 Brutal，也没有硬塞固定 100 Mbps 上传。仅在测试副本将 sing-box `up_mbps` 改为 0（不改用户配置），4 MB 对照上传三轮为 22.294 / 24.894 / 23.782 Mbps，同期 Shoes 为 28.017 / 13.996 / 14.336 Mbps；下载对照为 73.085 / 53.678 / 75.066，Shoes 为 48.521 / 63.609 / 46.754。中位数上传 23.782 对 14.336、下载 73.085 对 48.521，说明固定带宽策略并不是全部差距。短传输受建连、BBR 起速和两套 QUIC/BBR 实现差异影响；长传输与并发改善支持起速因素，但尚未逐项隔离 ACK 策略、初始拥塞窗口和调度的贡献，不作更强归因。测试期间未中断用户网络，链路波动仍存在。

### 复跑命令与验收边界

```sh
python native/bench-hy2.py outputs/shoes.exe --output outputs/hy2-repeat.json
python native/bench-hy2.py outputs/shoes.exe --bytes 32000000 --output outputs/hy2-repeat-long.json
python native/bench-hy2.py outputs/shoes.exe --parallel 4 --output outputs/hy2-repeat-parallel.json
```

默认读取 `D:/v2rayN/binConfigs/config.json` 的第一个 HY2 出站，SSH 别名为 `gc-jp`；可用 `--config`、`--reference`、`--ssh` 指定。目标必须是同一台测试服务器，否则证书校验会失败。每次三轮交替；`--reference-up-mbps 0` 是消融对照，不是用户的原配置。`--send-window` 仅供诊断；没有将 32 MiB 发送窗口带入交付。`--min-upload-ratio` 是可选回归门槛，失败时保留已验证的数据并返回非零；默认只记录，不把网络波动包装成通过。数据文件无密码、节点地址或证书内容。

实际本轮通过：全部上传/下载字节数和 SHA256、完整 HTTP 响应、4 路并发、断网超时后新建认证会话、错误密码拒绝、错误证书拒绝、进程和临时文件清理；GUI `go test -count=1 ./...`、`go vet ./...`、真实 `TestHY2Live`。后者真连接延迟 264 ms，只是这次测量，不作为带宽结论。已知配置失败经源码与报错核对仍为未转义的 Windows 路径，没有掩盖或修复无关测试。

交付结论：消除了约 0.5 Mbps 的严重上传塌速，长传输和并发恢复至数十 Mbps；**短连接仍落后，不是完整性能对齐版**。未复跑 Cloudflare 图形网页多连接测试，也未声称消除了所有抖动或在所有丢包条件下稳定。补丁从固定基线应用后与构建源码逐文件对照（仅归一化换行）验证；二进制 SHA256 与备份位置见 `outputs/hy2-build.json`。

## 回退

退出 Shoes，将备份的原版内核和 GUI 放回。原版内核不能加载 HY2 节点，回退前应先从当前配置中移除 HY2 节点。此次未修改服务器，也没有自动切换系统代理。
