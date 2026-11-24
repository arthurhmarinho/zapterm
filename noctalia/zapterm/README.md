# Widget do zapterm para o Noctalia

Mostra na barra quantas conversas têm mensagem não lida (sem contar
arquivadas e silenciadas), com a lista na dica. O clique abre o zapterm,
ou traz a janela dele para frente.

O clique abre o terminal escolhido no zapterm (`[interface] terminal` no
config, senão `$TERMINAL`, senão o primeiro instalado entre kitty, foot,
alacritty, wezterm, ghostty e os terminais padrão do Debian).

O daemon do zapterm atualiza o número sozinho sempre que ele muda; o
widget não fica consultando.

## Instalar

Precisa do Noctalia com plugins (API 30 ou mais nova) e do zapterm
instalado.

```sh
noctalia msg plugins source add zapterm path ~/caminho/do/zapterm/noctalia
noctalia msg plugins enable arthurhmarinho/zapterm
```

Depois ponha `"arthurhmarinho/zapterm:unread"` numa das listas da barra no
`~/.config/noctalia/config.toml`, por exemplo:

```toml
end = ["arthurhmarinho/zapterm:unread", "battery", "date", "user"]
```

## Opções

- **Caminho do zapterm** (padrão `zapterm`): usa o que estiver no PATH e,
  se o Noctalia não o enxergar lá, tenta `~/go/bin/zapterm`. Preencha com o
  caminho completo (`command -v zapterm`) se o seu estiver em outro lugar.
- **Esconder sem não lidas** (padrão: não).
