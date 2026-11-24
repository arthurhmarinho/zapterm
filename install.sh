#!/bin/sh
# Instala (ou atualiza) o zapterm: baixa o binário da última versão publicada
# no GitHub, confere o sha256 e põe em ~/.local/bin.
#
#   curl -fsSL https://raw.githubusercontent.com/arthurhmarinho/zapterm/main/install.sh | sh
#
# Variáveis opcionais:
#   ZAPTERM_BIN=/outra/pasta   onde instalar (padrão: ~/.local/bin)
#   ZAPTERM_VERSION=v0.1.0     uma versão específica, em vez da última
set -eu

repo="arthurhmarinho/zapterm"
base="${ZAPTERM_BASE:-https://github.com/$repo}" # só os testes trocam
bin_dir="${ZAPTERM_BIN:-$HOME/.local/bin}"

die() {
	printf 'zapterm: %s\n' "$*" >&2
	exit 1
}

for cmd in curl tar sha256sum install; do
	command -v "$cmd" >/dev/null 2>&1 || die "preciso do '$cmd' instalado"
done

[ "$(uname -s)" = "Linux" ] || die "só há versão para Linux"
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "arquitetura sem versão pronta: $(uname -m) (use: go install github.com/$repo/cmd/zapterm@latest)" ;;
esac

tag="${ZAPTERM_VERSION:-}"
if [ -z "$tag" ]; then
	# /releases/latest redireciona para /releases/tag/<tag>.
	url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$base/releases/latest") ||
		die "não consegui consultar $base/releases/latest"
	tag="${url##*/}"
fi
case "$tag" in
v[0-9]*) ;;
*) die "não achei nenhuma versão publicada em $base/releases" ;;
esac
version="${tag#v}"
file="zapterm_${version}_linux_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

printf 'baixando o zapterm %s (linux/%s)...\n' "$tag" "$arch"
curl -fsSL -o "$tmp/$file" "$base/releases/download/$tag/$file" || die "não consegui baixar $file"
curl -fsSL -o "$tmp/checksums.txt" "$base/releases/download/$tag/checksums.txt" || die "não consegui baixar checksums.txt"

(cd "$tmp" && sha256sum -c --ignore-missing checksums.txt >/dev/null 2>&1) ||
	die "o sha256 de $file não confere com o checksums.txt; nada foi instalado"

tar -xzf "$tmp/$file" -C "$tmp" zapterm || die "não achei o binário dentro de $file"

# Instala ao lado e troca por rename: dá para atualizar com o zapterm aberto
# (o processo em execução continua no arquivo antigo).
mkdir -p "$bin_dir"
install -m 0755 "$tmp/zapterm" "$bin_dir/.zapterm.new.$$"
mv -f "$bin_dir/.zapterm.new.$$" "$bin_dir/zapterm"
printf 'instalado: %s (%s)\n' "$bin_dir/zapterm" "$tag"

case ":$PATH:" in
*":$bin_dir:"*) zapterm="zapterm" ;;
*)
	zapterm="$bin_dir/zapterm"
	cat <<EOT

atenção: $bin_dir não está no seu PATH. Para usar só 'zapterm':
  bash/zsh: echo 'export PATH="$bin_dir:\$PATH"' >> ~/.bashrc   (ou ~/.zshrc)
  fish:     fish_add_path -g $bin_dir
EOT
	;;
esac

# Daemon rodando é o de antes da atualização: reinicia no binário novo.
sock="${ZAPTERM_SOCKET:-}"
[ -n "$sock" ] || sock="${XDG_RUNTIME_DIR:-/tmp}/zapterm.sock"
[ -S "$sock" ] || sock="/tmp/zapterm-$(id -u).sock"
if [ -S "$sock" ]; then
	printf '\no daemon está rodando; reiniciando na versão nova...\n'
	"$bin_dir/zapterm" restart || printf 'não deu para reiniciar sozinho; rode: %s restart\n' "$zapterm"
fi

printf '\npróximo passo: %s doctor   (mostra o que falta no sistema, com o pacote de cada distro)\n' "$zapterm"
