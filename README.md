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
  (Tudo · Não lidas · Grupos · Canais), arquivadas e busca, inclusive no
  texto das mensagens (sem diferenciar acento e maiúscula), pulando para a
  mensagem achada.
- **Mensagens:** responder com citação, reagir (❤️ 👍 😂 😮 😢 🙏),
  copiar, editar, apagar para todos e encaminhar; formatação do WhatsApp
  (*negrito*, _itálico_, ~riscado~, `código`) e links clicáveis;
  mensagens editadas e apagadas, tracinhos de entregue e lido,
  "digitando…", online e visto por último.
- **Grupos:** marcar alguém com `@` (lista de participantes enquanto você
  digita) e ver as menções em destaque; menção a você notifica mesmo em
  grupo silenciado.
- **Organizar:** fixar, arquivar, silenciar e marcar como lida pelo
  próprio zapterm (sincroniza com o celular); nova conversa digitando um
  número na busca.
- **Mídia no terminal:** fotos, figurinhas (inclusive animadas) e
  miniaturas de vídeo dentro do balão, pelo protocolo gráfico do kitty.
  Foto abre em tela cheia; vídeo e PDF abrem no seu player e leitor.
- **Enviar:** texto, print colado com `ctrl+v`, qualquer arquivo pelo
  seletor nativo (foto, vídeo, áudio, documento), suas figurinhas
  favoritas do celular e mensagem de voz gravada no microfone.
- **Notificações** com a foto do contato, respeitando chats silenciados e
  arquivados e sem avisar do chat que você está vendo. Clicar abre o chat
  no zapterm (ou abre o zapterm, se estiver fechado); servidores com
  resposta embutida (Noctalia, Plasma) deixam responder pela própria
  notificação.
- **Barra do Noctalia:** widget com o número de conversas não lidas, que
  abre o zapterm no clique ([instruções](noctalia/zapterm/README.md)).
  Fora do Noctalia, `zapterm unread --json` dá o mesmo resumo para outras
  barras.
- **Perfil** (clique no cabeçalho ou `alt+i`): foto grande, número e
  recado do contato, ou descrição e participantes do grupo.
- **Mídia, links e docs** (`enter` no perfil): abas Fotos, Vídeos, Docs,
  Links e Áudios, separadas por mês; `ctrl+g` mostra o item na conversa.
  Mídia antiga sem prévia é pedida de novo ao seu celular.

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
O mouse também funciona (clique, botão direito para o menu da mensagem ou do chat, roda).

| Tecla | Faz |
|---|---|
| `↑` `↓` | chats na lista, ou mensagens na conversa |
| `tab` | troca entre lista e conversa |
| `enter` | envia (com o campo vazio na lista, abre o chat) |
| `shift+enter` | nova linha |
| `ctrl+p` | pesquisar conversa (ou digite um número para conversa nova) |
| `ctrl+f` | procurar nesta conversa: enter/↑ vai ao mais antigo, ↓ ao mais novo |
| `alt+↑` / `alt+↓` | abre a conversa de cima / de baixo na lista |
| `alt+n` | abre a próxima conversa com mensagens não lidas |
| `alt+1`…`alt+4`, `alt+a` | filtros e arquivadas |
| `ctrl+r` | responder a mensagem selecionada |
| `ctrl+e` | reagir: 1–6 as rápidas, ou digite o nome de qualquer emoji (`fogo` → 🔥) |
| `ctrl+↑` | editar a sua última mensagem |
| `:nome` | emoji pelo nome no campo: `:kkk` → 😂, `:cora` → ❤️ (português ou inglês) |
| `ctrl+o` | abrir foto, vídeo, documento, link, ou tocar áudio |
| `alt+c` `alt+e` | copiar / editar (sua, até 15 min) |
| `alt+.` | menu da mensagem: responder, responder em privado, reagir, abrir, copiar, editar, encaminhar, apagar (ou botão direito) |
| `alt+d` | apagar: pergunta se é para você ou para todos; `alt+z` desfaz o "para mim" por 5 s |
| `alt+w` | encaminhar |
| `alt+s` | favoritar / desfavoritar a mensagem (sincroniza com o celular); o filtro **★** (`alt+5`) lista as favoritas |
| `alt+m` | menu do chat: fixar, arquivar, silenciar (ou botão direito) |
| `alt+i` | perfil do contato ou grupo (ou clique no cabeçalho) |
| `@` | marcar alguém num grupo |
| `ctrl+v` | colar print para enviar |
| `alt+f` | anexar arquivo |
| `ctrl+s` | figurinhas (favoritas do celular; `tab` troca para as recentes) |
| `ctrl+t` | gravar mensagem de voz (espaço para e deixa ouvir antes de enviar) |
| `ctrl+o` no áudio | toca / pausa (com mpv instalado; sem ele, toca do começo ao fim) |
| `alt+v` | velocidade do áudio: 1×, 1,5×, 2× |
| `←` / `→` | com áudio tocando e o campo vazio, volta / avança 5 s |
| `alt+x` | para o áudio |
| `pgup` `pgdn` | rolar a conversa |
| `esc` | cancela o que estiver aberto |
| `ctrl+c` | limpa o campo; vazio, duas vezes, sai |
| `F1` | todos os atalhos (os atalhos podem ser trocados no [config](#configuração)) |

## Configuração

```sh
zapterm config
```

Cria `~/.config/zapterm/config.toml` todo comentado, com os valores padrão,
e mostra o caminho. Sem o arquivo, vale o padrão. Dá para:

- **[privacy]**: não mandar o tracinho azul ao abrir um chat, não aparecer
  online nem "digitando…", notificação sem o texto da mensagem, desligar
  notificações;
- **[keys]**: trocar qualquer atalho (a ajuda e o rodapé acompanham);
- **[colors]**: cores do destaque, dos balões, dos painéis (número ANSI ou
  hex);
- **[apps]**: programa para vídeo, PDF, imagem e links;
- **[interface]**: imagens, ícones da Nerd Font, largura da lista.

Mudanças valem ao reabrir o zapterm; as de privacidade também pedem
reiniciar o daemon (`kill $(pgrep -f 'zapterm daemon$')`, ele sobe sozinho
de novo).

## Limitações

- Chamadas de voz e vídeo não são suportadas.
- Status (stories) ficam de fora.
- Mídia, citações e reações das mensagens que vieram no pareamento (o
  histórico inicial) não estão disponíveis; só as que chegam depois.
- Figurinhas favoritas antigas costumam ter expirado no servidor do
  WhatsApp (o celular guarda cópia local); só as que ainda existem lá, ou
  que passaram por uma conversa, aparecem.
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

Os nomes dos emojis (`internal/tui/emoji.tsv`) vêm do
[CLDR](https://cldr.unicode.org/) e da lista `emoji-test.txt` do Unicode,
© Unicode, Inc., sob a [Licença Unicode v3](https://www.unicode.org/license.txt).
