# Shoes GUI

Windows 上给 [cfal/shoes](https://github.com/cfal/shoes) 内核做的图形界面，交互对齐 FlClash：仪表盘、节点分组、订阅、测速、托盘。界面用 [MyGo](https://github.com/egoist/mygo)。

- `work/`：GUI 源码（Go）
- `outputs/使用说明.md`：功能和用法
- `outputs/验证结果.md`：实测记录
- `outputs/licenses/`：第三方许可证
- `方案-对齐FlClash.md`：设计方案

## 构建

```sh
cd work
go test ./...
go vet ./...
go build -trimpath -ldflags="-H windowsgui -s -w" -o ../outputs/ShoesGUI.exe .
```

运行时需要把 shoes 内核（`shoes.exe`，Windows x64，0.3.4）放在 `ShoesGUI.exe` 旁边。exe 不进仓库。
