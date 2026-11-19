# zapterm

WhatsApp no terminal. Lista de conversas, balões, fotos e figurinhas
desenhadas no próprio terminal (kitty), áudio, notificações e atalhos de
teclado, com o visual do WhatsApp Web e as cores do seu tema.

![zapterm em modo demonstração](docs/demo.png)

> **Aviso:** o zapterm é um cliente **não oficial**. Ele se conecta como um
> "aparelho conectado", do mesmo jeito que o WhatsApp Web, pela biblioteca
> [whatsmeow](https://github.com/tulir/whatsmeow). Usar cliente não oficial
> viola os Termos de Serviço do WhatsApp, e a Meta pode banir o número.
> Com uso pessoal normal o risco é baixo, mas não é zero. Não use para
> mandar mensagem em massa nem para automação.

## O que ele faz

- **Conversas:** lista com fotos de perfil, fixados, não lidas, filtros
  (Tudo · Não lidas · Grupos · Canais), arquivadas e busca.
- **Mensagens:** responder com citação, reagir (❤️ 👍 😂 😮 😢 🙏), ver
  mensagens editadas e apagadas, tracinhos de entregue e lido,
  "digitando…", online e visto por último.
- **Mídia no terminal:** fotos, figurinhas (inclusive animadas) e
  miniaturas de vídeo dentro do balão, pelo protocolo gráfico do kitty.
  Foto abre em tela cheia; vídeo e PDF abrem no seu player e leitor.
- **Enviar:** texto, print colado com `ctrl+v`, qualquer arquivo pelo
  seletor nativo (foto, vídeo, áudio, documento), figurinhas recentes e
  mensagem de voz gravada no microfone.
- **Notificações** com a foto do contato, respeitando chats silenciados e
  arquivados e sem avisar do chat que você está vendo.

## Como funciona

Dois processos: um **daemon** fica conectado ao WhatsApp o tempo todo
(notificações chegam mesmo com a tela fechada) e guarda conversas num
SQLite local. A **TUI** fala com ele por um socket Unix e sobe o daemon
sozinha se ele não estiver rodando.

Seus dados ficam só na sua máquina:

| O quê | Onde |
|---|---|
| Sessão do WhatsApp e conversas | `~/.local/share/zapterm/` |
| Fotos, áudios, figurinhas e avatares baixados (pode apagar) | `~/.cache/zapterm/` |
| Log do daemon | `~/.local/share/zapterm/daemon.log` |

Privacidade: o daemon só aparece **online** para os outros enquanto a TUI
está aberta e com a janela em foco num chat, como o WhatsApp Web. Abrir um
chat marca as mensagens como lidas, como no app.

## Requisitos

Feito e testado no Linux (Wayland, Sway), no terminal **kitty**.

| Para | Precisa de |
|---|---|
| Compilar | Go 1.27+ e um compilador C (o SQLite usa cgo) |
| Fotos e figurinhas no terminal | kitty (em outro terminal elas viram texto) |
| Ícones | uma [Nerd Font](https://www.nerdfonts.com) (`ZAPTERM_NERDFONT=0` troca por texto) |
| Áudio (ouvir e gravar) | GStreamer com os plugins base, good e de Opus; PipeWire ou PulseAudio |
| Colar print | `wl-paste` (wl-clipboard) |
| Anexar arquivo | `xdg-desktop-portal` com um backend (gtk, gnome, kde…) |
| Notificações | `notify-send` e um servidor de notificação |
| Vídeo e PDF | mpv/vlc/celluloid e zathura/sioyek/evince (usa o primeiro instalado) |

## Instalação

```sh
go install github.com/arthurhmarinho/zapterm/cmd/zapterm@latest
```

O binário vai para `~/go/bin` (ou o `GOBIN`). Se `zapterm` não for
encontrado, ponha essa pasta no PATH. No fish:

```fish
fish_add_path -g $HOME/go/bin
```

No bash/zsh: `export PATH="$HOME/go/bin:$PATH"` no `~/.bashrc` ou `~/.zshrc`.

Para atualizar, rode o mesmo `go install` de novo.

## Primeiro uso

```sh
zapterm login
```

Escaneie o QR code no celular: **WhatsApp → Aparelhos conectados →
Conectar um aparelho**. Deixe o comando rodando até o log parar de mostrar
`history sync` (alguns minutos): é quando o histórico chega. Depois:

```sh
zapterm
```

Para o daemon subir junto com o Sway (e as notificações chegarem sem
abrir a TUI), no `~/.config/sway/config`, com o caminho completo, porque o
Sway não lê o PATH do shell:

```
exec ~/go/bin/zapterm daemon
```

Para ver sem conta nenhuma, com conversas inventadas:

```sh
zapterm demo
```

## Atalhos

O campo de escrita fica sempre ativo, como no Claude Code: é só digitar.
O mouse também funciona (clique, botão direito para responder, roda).

| Tecla | Faz |
|---|---|
| `↑` `↓` | chats na lista, ou mensagens na conversa |
| `tab` | troca entre lista e conversa |
| `enter` | envia (com o campo vazio na lista, abre o chat) |
| `shift+enter` | nova linha |
| `ctrl+p` | pesquisar conversa |
| `alt+1`…`alt+4`, `alt+a` | filtros e arquivadas |
| `ctrl+r` | responder a mensagem selecionada |
| `ctrl+e` | reagir |
| `ctrl+o` | abrir foto, vídeo, documento, ou tocar áudio |
| `ctrl+v` | colar print para enviar |
| `alt+f` | anexar arquivo |
| `ctrl+s` | figurinhas recentes |
| `ctrl+t` | gravar mensagem de voz |
| `pgup` `pgdn` | rolar a conversa |
| `esc` | cancela o que estiver aberto |
| `ctrl+c` | limpa o campo; vazio, duas vezes, sai |

## Limitações

- Chamadas de voz e vídeo não são suportadas.
- Status (stories) ficam de fora.
- Mídia, citações e reações das mensagens que vieram no pareamento (o
  histórico inicial) não estão disponíveis; só as que chegam depois.
- Figurinhas do seletor são as que passaram pelas suas conversas; os
  pacotes e favoritas do celular não ficam acessíveis.
- Em grupo, o tracinho azul aparece quando pelo menos uma pessoa leu.

## Comandos de depuração

```sh
zapterm chats              # lista os chats
zapterm msgs <jid> [n]     # últimas mensagens de um chat
zapterm tail               # eventos ao vivo
zapterm media <jid> <id>   # baixa a mídia de uma mensagem
```

## Licença

[GPL-3.0](LICENSE). Usa o [whatsmeow](https://github.com/tulir/whatsmeow)
(MPL-2.0) e o [Bubble Tea](https://github.com/charmbracelet/bubbletea)
(MIT).
