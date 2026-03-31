#!/bin/sh

set -eu

usage() {
	cat <<'EOF'
Usage:
  ./build-ipk.sh --openwrt-dir PATH [--output-dir PATH] [--jobs N] [--verbose] [--no-clean]

Options:
  --openwrt-dir PATH  OpenWrt SDK or source tree root
  --output-dir PATH   Directory to copy built ipk into
  --jobs N            Parallel make jobs
  --verbose           Build with V=s
  --no-clean          Skip package/kc-go/clean before compile
  -h, --help          Show this help
EOF
}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
template_dir="$script_dir/package/kc-go"
output_dir="$script_dir/dist"
openwrt_dir="${OPENWRT_DIR:-}"
verbose_flag=""
clean_first=1
jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '1')

while [ "$#" -gt 0 ]; do
	case "$1" in
		--openwrt-dir)
			[ "$#" -ge 2 ] || {
				printf 'missing value for %s\n' "$1" >&2
				exit 1
			}
			openwrt_dir=$2
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
		--jobs|-j)
			[ "$#" -ge 2 ] || {
				printf 'missing value for %s\n' "$1" >&2
				exit 1
			}
			jobs=$2
			shift 2
			;;
		--verbose)
			verbose_flag="V=s"
			shift
			;;
		--no-clean)
			clean_first=0
			shift
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

[ -n "$openwrt_dir" ] || {
	printf 'missing --openwrt-dir\n' >&2
	usage >&2
	exit 1
}

openwrt_dir=$(CDPATH= cd -- "$openwrt_dir" && pwd)
output_dir=$(mkdir -p "$output_dir" && CDPATH= cd -- "$output_dir" && pwd)

[ -f "$openwrt_dir/rules.mk" ] || {
	printf 'not an OpenWrt tree: %s\n' "$openwrt_dir" >&2
	exit 1
}
[ -f "$openwrt_dir/include/package.mk" ] || {
	printf 'OpenWrt package helpers not found in: %s\n' "$openwrt_dir" >&2
	exit 1
}
[ -f "$openwrt_dir/.config" ] || {
	printf 'OpenWrt tree is not configured: %s/.config missing\n' "$openwrt_dir" >&2
	exit 1
}

package_dir="$openwrt_dir/package/kc-go"
src_dir="$package_dir/src"

printf 'Staging package into %s\n' "$package_dir"
rm -rf "$package_dir"
mkdir -p "$package_dir"
cp -R "$template_dir"/. "$package_dir"/

mkdir -p "$src_dir"
cp "$repo_dir"/go.mod "$src_dir"/
cp "$repo_dir"/go.sum "$src_dir"/
cp "$repo_dir"/*.go "$src_dir"/
cp -R "$repo_dir"/pkg "$src_dir"/

printf 'Building kc-go ipk in %s\n' "$openwrt_dir"

if [ "$clean_first" -eq 1 ]; then
	(
		cd "$openwrt_dir"
		if [ -n "$verbose_flag" ]; then
			make -j"$jobs" "$verbose_flag" package/kc-go/clean
		else
			make -j"$jobs" package/kc-go/clean
		fi
	)
fi

(
	cd "$openwrt_dir"
	if [ -n "$verbose_flag" ]; then
		make -j"$jobs" "$verbose_flag" package/kc-go/compile
	else
		make -j"$jobs" package/kc-go/compile
	fi
)

rm -f "$output_dir"/kc-go_*.ipk
found=0
for ipk in "$openwrt_dir"/bin/packages/*/*/kc-go_*.ipk; do
	[ -e "$ipk" ] || continue
	cp "$ipk" "$output_dir"/
	printf 'Generated: %s\n' "$ipk"
	found=1
done

[ "$found" -eq 1 ] || {
	printf 'build finished but no kc-go ipk was found\n' >&2
	exit 1
}

printf 'Copied ipk to %s\n' "$output_dir"
