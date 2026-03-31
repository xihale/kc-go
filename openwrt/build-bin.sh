#!/bin/sh

set -eu

usage() {
	cat <<'EOF'
Usage:
  ./build-bin.sh --arch ARCH [--output PATH] [--output-dir PATH] [--name NAME]

Options:
  --arch, --target ARCH  OpenWrt arch or Go arch target
  --output PATH          Exact output file path
  --output-dir PATH      Output directory for the built binary
  --name NAME            Output filename when using --output-dir
  -h, --help             Show this help

Examples:
  ./build-bin.sh --arch mipsel_24kc
  ./build-bin.sh --arch aarch64_cortex-a53
  ./build-bin.sh --arch x86_64 --output ./dist/kc-go

Notes:
  - This project is pure Go, so the script forces CGO_ENABLED=0.
  - Common OpenWrt arch names like mipsel_24kc and aarch64_cortex-a53 are supported.
EOF
}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
target_arch=""
output_path=""
output_dir="$script_dir/dist/bin"
output_name=""

while [ "$#" -gt 0 ]; do
	case "$1" in
		--arch|--target)
			[ "$#" -ge 2 ] || {
				printf 'missing value for %s\n' "$1" >&2
				exit 1
			}
			target_arch=$2
			shift 2
			;;
		--output)
			[ "$#" -ge 2 ] || {
				printf 'missing value for %s\n' "$1" >&2
				exit 1
			}
			output_path=$2
			shift 2
			;;
		--output-dir)
			[ "$#" -ge 2 ] || {
				printf 'missing value for %s\n' "$1" >&2
				exit 1
			}
			output_dir=$2
			shift 2
			;;
		--name)
			[ "$#" -ge 2 ] || {
				printf 'missing value for %s\n' "$1" >&2
				exit 1
			}
			output_name=$2
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			printf 'unknown argument: %s\n' "$1" >&2
			usage >&2
			exit 1
			;;
	esac
done

[ -n "$target_arch" ] || {
	printf 'missing --arch\n' >&2
	usage >&2
	exit 1
}

goos=linux
goarch=""
goarm=""
gomips=""
gomips64=""
goamd64=""

case "$target_arch" in
	aarch64*|arm64)
		goarch=arm64
		;;
	x86_64|x86-64|amd64)
		goarch=amd64
		goamd64=v1
		;;
	i386|i486|i586|i686|x86|386|i386_pentium4)
		goarch=386
		;;
	riscv64*)
		goarch=riscv64
		;;
	arm_arm1176jzf-s*|arm1176*|armv6*|arm_v6)
		goarch=arm
		goarm=6
		;;
	arm_arm926ej-s*|arm926*|armv5*|arm_v5|arm_xscale*|xscale)
		goarch=arm
		goarm=5
		;;
	arm*|arm_v7|armv7*|armv8l*)
		goarch=arm
		goarm=7
		;;
	mipsel*|mipsle)
		goarch=mipsle
		gomips=softfloat
		;;
	mips64el*|mips64le)
		goarch=mips64le
		gomips64=hardfloat
		;;
	mips64*|mips64)
		goarch=mips64
		gomips64=hardfloat
		;;
	mips*|mips)
		goarch=mips
		gomips=softfloat
		;;
	*)
		printf 'unsupported arch: %s\n' "$target_arch" >&2
		printf 'try values like mipsel_24kc, aarch64_cortex-a53, arm_cortex-a7, x86_64\n' >&2
		exit 1
		;;
esac

if [ -z "$output_path" ]; then
	mkdir -p "$output_dir"
	if [ -z "$output_name" ]; then
		output_name="kc-go-$target_arch"
	fi
	output_path="$output_dir/$output_name"
else
	mkdir -p "$(dirname -- "$output_path")"
fi

printf 'Building kc-go for %s\n' "$target_arch"
printf '  GOOS=%s GOARCH=%s' "$goos" "$goarch"
if [ -n "$goarm" ]; then
	printf ' GOARM=%s' "$goarm"
fi
if [ -n "$gomips" ]; then
	printf ' GOMIPS=%s' "$gomips"
fi
if [ -n "$gomips64" ]; then
	printf ' GOMIPS64=%s' "$gomips64"
fi
if [ -n "$goamd64" ]; then
	printf ' GOAMD64=%s' "$goamd64"
fi
printf '\n'

(
	cd "$repo_dir"
	CGO_ENABLED=0 \
	GOOS="$goos" \
	GOARCH="$goarch" \
	GOARM="$goarm" \
	GOMIPS="$gomips" \
	GOMIPS64="$gomips64" \
	GOAMD64="$goamd64" \
	go build -trimpath -ldflags='-s -w' -o "$output_path" .
)

printf 'Built binary: %s\n' "$output_path"
