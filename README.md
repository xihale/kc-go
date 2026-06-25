# kc-go

`kc-go` 是一个面向 OpenWrt 的常驻网络保活程序。

它会定时检测连通性，在检测到门户认证或断网时自动尝试重连，并可在网络恢复后更新 Cloudflare DDNS 记录。

## 功能

- 定时检测本机 IP，IP 变化或掉线后自动执行门户认证登录
- 支持 Cloudflare `A` / `AAAA` 记录自动更新
- 支持 `procd` 自启动、后台常驻、日志落盘
- 支持 `log` 命令彩色跟随日志输出

## 命令

```sh
kc-go run [-c PATH] [-f]
kc-go install [-c PATH]
kc-go uninstall [-p]
kc-go log [-c PATH] [-n LINES]
```

说明：

- `run` 运行主服务，`-f` 以前台方式运行（不 daemonize）
- `install` 安装到 OpenWrt 并注册 `/etc/init.d/kc-go`
- `uninstall` 删除程序和 init 脚本，默认保留配置和日志
- `uninstall -p` 连配置和日志一起清理
- `log` 类似 `tail -f`，并按日志级别着色

## 配置文件

默认配置文件路径：`/etc/kc-go/config.yaml`

开发环境下如果 `/etc/kc-go/config.yaml` 不存在，会回退到当前目录的 `config.yaml`。

示例：

```yaml
service:
  log_file: "/var/log/kc-go.log"

check:
  url: "http://connect.rom.miui.com/generate_204"
  interval: 1

account:
  user: "YOUR_ACCOUNT"
  password: "YOUR_PASSWORD"

cloudflare:
  token: "YOUR_CLOUDFLARE_API_TOKEN"
  zone_id: "YOUR_ZONE_ID"
  domains:
    - name: "g.xihale.top"
      type: "A"
    - name: "g6.xihale.top"
      type: "AAAA"
```

说明：

- 只需要填 `token`、`zone_id`、域名和记录类型
- 不需要填 Cloudflare `record_id`
- 程序会按 `name + type` 自动查询并更新对应记录

Cloudflare Token 建议至少具备：

- `Zone:Read`
- `DNS:Edit`

## OpenWrt 快速部署

不想折腾 SDK 时，推荐直接交叉编译纯 Go 二进制。

```sh
./openwrt/build-bin.sh --arch aarch64_generic
scp ./openwrt/dist/bin/kc-go-aarch64_generic root@router:/tmp/kc-go
scp ./config.yaml root@router:/tmp/config.yaml
ssh root@router 'chmod +x /tmp/kc-go && /tmp/kc-go install -c /tmp/config.yaml'
```

已知常见架构：

- `aarch64_generic`
- `aarch64_cortex-a53`
- `arm_cortex-a7`
- `mipsel_24kc`
- `mips_24kc`
- `x86_64`

安装完成后：

```sh
ssh root@router '/usr/bin/kc-go log'
```

更多 OpenWrt 打包说明见 `openwrt/README.md`。
