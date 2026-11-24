#!/bin/sh
# Pós-instalação do .deb: aponta o apt para o repositório do zapterm, para as
# versões novas chegarem pelo Gerenciador de Atualizações. Não roda apt update
# (o dpkg está travado enquanto este script roda); o próximo update já enxerga.
set -e

list=/etc/apt/sources.list.d/zapterm.list

if [ "$1" = "configure" ] && [ ! -e "$list" ]; then
	echo "deb [signed-by=/usr/share/keyrings/zapterm-archive-keyring.gpg] https://arthurhmarinho.github.io/zapterm/deb ./" >"$list"
fi

exit 0
