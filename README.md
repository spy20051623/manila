# Manila

Manila 是一个用 Go 实现的《马尼拉》电子版后端与网页客户端。服务端负责权威规则、房间、对局状态、合法动作、内置 AI 行动和前端静态资源托管。

## 运行

开发环境可以直接启动服务端：

```sh
go run ./cmd/server
```

默认监听地址是 `localhost:18080`，启动后在浏览器访问：

```text
http://localhost:18080/
```

监听地址配置优先级如下：

1. `MANILA_ADDR` 环境变量。
2. 当前工作目录下的 `config.json`，例如：

   ```json
   {
     "listenAddr": "localhost:18080"
   }
   ```

3. 默认值 `localhost:18080`。

发布二进制已经嵌入前端静态资源，可以直接运行。若要使用训练后的 AI 权重，运行目录需要能访问 `ai_weights_current`，其中至少包含当前权重文件。

## 构建发布版

仓库提供两个等价的构建入口，都会一次性构建服务端在 Windows/Linux、AMD64/ARM64 四个平台的二进制文件。

Windows PowerShell：

```powershell
.\scripts\build-release.ps1
```

Linux、macOS、Git Bash 或 WSL：

```sh
sh ./scripts/build-release.sh
```

输出目录固定为：

```text
bin/release/windows-amd64/manila-server.exe
bin/release/windows-arm64/manila-server.exe
bin/release/linux-amd64/manila-server
bin/release/linux-arm64/manila-server
```

构建脚本每次都会重建 `bin/release`，避免旧产物混入。脚本只构建 `cmd/server`，不会构建 AI 训练或评测命令。

## 测试

运行全部 Go 测试：

```sh
go test ./...
```

## 文档

- [游戏规则说明](./MANILA_RULES.md)
- [后端实现设计](./MANILA_BACKEND_DESIGN.md)
- [AI 参数设计](./MANILA_AI_DESIGN.md)
