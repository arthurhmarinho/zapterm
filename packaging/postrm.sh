#!/bin/sh
# Pós-remoção do .deb: no purge, tira o repositório do apt. Remover sem purge
# mantém o .list (como qualquer arquivo de configuração).
set -e

if [ "$1" = "purge" ]; then
	rm -f /etc/apt/sources.list.d/zapterm.list
fi

exit 0
