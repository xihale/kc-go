# kc-go OpenWrt package

最省事的方式是直接交叉编译纯 Go 二进制，不走 SDK：`scripts/kc-go/openwrt/build-bin.sh`。

典型命令：

```sh
./scripts/kc-go/openwrt/build-bin.sh --arch aarch64_generic
```

常见架构示例：

- `mipsel_24kc`
- `mips_24kc`
- `arm_cortex-a7`
- `aarch64_generic`
- `aarch64_cortex-a53`
- `x86_64`

脚本会输出到 `scripts/kc-go/openwrt/dist/bin`，然后你可以把产物传到路由器上执行：

```sh
scp ./scripts/kc-go/openwrt/dist/bin/kc-go-aarch64_generic root@router:/tmp/kc-go
scp ./scripts/kc-go/config.yaml root@router:/tmp/config.yaml
ssh root@router 'chmod +x /tmp/kc-go && /tmp/kc-go install --config /tmp/config.yaml'
```

Cloudflare 配置只需要填 `token`、`zone_id` 和域名列表，不需要填 `record_id`。

如果你确实要产出 `.ipk`，再用 `scripts/kc-go/openwrt/build-ipk.sh`，它会把打包模板和源码快照同步到 OpenWrt 树里，再执行编包。

典型构建命令：

```sh
./scripts/kc-go/openwrt/build-ipk.sh --openwrt-dir /path/to/openwrt --verbose
```

脚本默认会：

- 同步包模板到 OpenWrt 的 `package/kc-go`
- 同步当前 `kc-go` 源码到 `package/kc-go/src`
- 执行 `make package/kc-go/clean` 和 `make package/kc-go/compile`
- 把生成的 `.ipk` 复制到 `scripts/kc-go/openwrt/dist`

可选参数：

- `--output-dir PATH` 自定义导出目录
- `--jobs N` 指定并行编译数
- `--no-clean` 跳过清理步骤
- `--verbose` 透出 `V=s` 日志

这个包会安装：

- `/usr/bin/kc-go`
- `/etc/init.d/kc-go`
- `/etc/kc-go/config.yaml`

构建依赖默认使用 OpenWrt `packages` feed 里的 Go 工具链定义。

## TODO

- 后续计划补一个 `upgrade`/`reinstall` 流程，用于保留配置并平滑更新二进制与 init 脚本。
