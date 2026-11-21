# Roteiro do zapterm

Levantado em 20/11/2026: três agentes pesquisaram as funcionalidades do
WhatsApp Web e Desktop (cerca de 170, com fonte oficial) e dois agentes
filtraram o que é possível no código (whatsmeow + zapterm) e o que faz
sentido num terminal. Esforço: **P** até ~1 h · **M** meio dia · **G** um
dia ou mais.

Marque `[x]` conforme for entrando (`[~]` = feito em parte).

## 0. Problemas encontrados (corrigir primeiro)

- [x] **Mensagens temporárias ignoradas no envio.** Numa conversa com
  mensagens temporárias, o que sai pelo zapterm não some: o envio não
  leva `ContextInfo.Expiration`. Prazo vem de
  `Conversation.GetEphemeralExpiration()` (histórico),
  `GroupInfo.GroupEphemeral` e `ProtocolMessage_EPHEMERAL_SETTING`. (M)
- [x] **Eventos e recados de vídeo descartados.** `describe()` não trata
  `EventMessage` nem `PtvMessage`; a mensagem some sem marcador. (P)
- [x] **Rascunho vaza entre conversas.** `open()` não guarda nem limpa o
  campo de escrita; o texto vai junto para o próximo chat. (P)
- [x] **Conversas trancadas aparecem.** O celular esconde com código
  secreto; o zapterm mostra. `LockChatAction.GetLocked()` no índice
  `appstate.IndexLock`, que já chega em `events.AppState`. (P)

## 1. Próxima leva — ler direito o que chega e tirar atrito

### Mostrar o que hoje vira marcador
- [x] **Evento**: 📅 data/hora, local, link de chamada, contagem
  Vou/Talvez/Não vou. `waE2E.EventMessage`. (P)
- [x] **Contato (vCard)**: "👤 Nome · +55 …"; `ctrl+o` abre a conversa,
  `alt+c` copia o número. `ContactMessage.GetVcard()`,
  `ContactsArrayMessage`. (P)
- [x] **Localização**: endereço ou coordenadas, link OSC 8 para o
  OpenStreetMap. `LocationMessage`, `LiveLocationMessage`. (P)
- [~] **Recado de vídeo** (feito: aparece e abre como vídeo; falta a miniatura redonda): miniatura redonda; `ctrl+o` abre no player.
  `PtvMessage`. (P)
- [ ] **GIF animado no balão**, como as figurinhas animadas.
  `VideoMessage.GetGifPlayback()`. (M)
- [~] **Chamada perdida / recebida** (feito: linha "chamada recebida · atenda no celular" e notificação; falta o botão "recusar" e saber se foi perdida): linha de sistema "📞 chamada perdida
  de Fulano" e notificação com "recusar". `events.CallOffer`,
  `CallTerminate`, `cli.RejectCall`, `CallLogMessage`. (P)
- [x] **Prévia de link recebida**: cartão com título, domínio e
  miniatura. `ExtendedTextMessage` `GetTitle`/`GetDescription`/
  `GetJPEGThumbnail`. (P)
- [x] **Listas (`*`, `-`, `1.`) e citação (`> `)** na formatação. (P)
- [x] **Rótulo "Encaminhada"** no balão. `ContextInfo.GetIsForwarded()`. (P)
- [x] **Visualização única recebida**: marcador "👁 abra no celular" (o
  WhatsApp não entrega o conteúdo a aparelho conectado). (P)
- [x] **Aviso de mensagens temporárias** no cabeçalho (⏱ 7d). (P)

### Galeria do perfil (pedido do Arthur em 21/11)
Hoje a grade está ruim: células fixas de 14×7 encaixam a imagem inteira,
então prints deitados viram uma faixa fina e fotos em pé ficam minúsculas;
mídia sem miniatura vira um quadrado vazio com ícone; só cabem 2 linhas
(mesmo com "Mídia (29)") e sobra tela vazia; não há data, remetente nem
filtro.
- [x] **Miniaturas que preenchem a célula** (cortar no centro, como o
  WhatsApp), células maiores e quadradas, e a grade ocupando a altura
  livre, com rolagem. (M)
- [x] **Filtro por tipo**: abas `Fotos · Vídeos · Docs · Links · Áudios`
  (`tab`/`shift+tab` ou clique). Fotos e vídeos em grade (vídeo com ▶ e
  duração); docs, links e áudios em lista com nome, remetente e data;
  `enter` abre. Tudo sai do SQLite (`kind` e links no texto). (M)
- [x] **Separar por mês** ("Novembro", "Outubro"…), como no app. (P)
- [x] **Sem miniatura**: pedir ao celular de novo
  (`cli.SendMediaRetryReceipt`) ou baixar sob demanda ao ficar visível,
  em vez do quadrado vazio. (M)

### Enquetes
- [~] **Ver**: (feito: pergunta e opções num cartão; falta a contagem de votos) opções com barra (`████░░ 4`), ✓ nas suas, total.
  `PollCreationMessage`, `cli.DecryptPollVote`,
  `WebMessageInfo.GetPollUpdates()`. Precisa de tabela de votos. (M)
- [ ] **Votar**: `ctrl+o` na enquete abre painel acima da caixa; números
  ou espaço marcam, `enter` vota. `cli.BuildPollVote`. (P)

### Áudio
- [ ] **Velocidade** 1× → 1,5× → 2× em `alt+v`, lembrada para os
  próximos. `mpv --speed` ou gstreamer `scaletempo`. (P–M)
- [ ] **Pausar** e **avançar/voltar** (±5 s, clique na barra). (M)
- [ ] **Ouvir antes de enviar** e **pausar a gravação** (`p`, `o` no
  painel de gravação). (P / M)
- [ ] **Tocar o próximo áudio sozinho** e mini player no rodapé ao
  trocar de conversa. (M)

### Navegação
- [ ] **Divisor "── N não lidas ──"**: o chat abre ali, não no fim. (P)
- [ ] **Busca dentro da conversa** (barra acima da caixa, "termo 3/12",
  destaque). (P) — depende da decisão 2.
- [ ] **`alt+↑/↓`** troca de chat; **`alt+n`** vai para a próxima não
  lida. (P)

### Campo de escrita
- [x] **Rascunho por conversa** (texto, resposta e anexo), "Rascunho: …"
  na lista. (P) — junto com o problema do item 0.
- [ ] **Emoji pelo nome**: `:ri` → 😂 🤣, como o `@`, com recentes. (M)
- [ ] **Reagir com qualquer emoji** (mesmo componente) e **ver quem
  reagiu** (já está no banco). (M / P)
- [ ] **`ctrl+↑` edita a sua última mensagem.** (P)

## 2. Depois — menu da mensagem e o que mora nele

- [ ] **Menu da mensagem (`alt+.`)**: lista numerada só com o que vale
  para aquela mensagem. (M)
- [ ] **Responder em privado** (`alt+r`): abre a conversa individual com a
  citação. `ContextInfo{StanzaID, Participant, RemoteJID: grupo}`. (P)
- [ ] **Apagar para mim**, com "alt+z desfaz" por 5 s. Patch em
  `appstate.IndexDeleteMessageForMe`; receber `events.DeleteForMe`. (P–M)
- [ ] **Favoritar (★)** em `alt+s` e linha "★ Favoritas" na lista.
  `appstate.BuildStar`, `events.Star`, `WebMessageInfo.GetStarred()`. (P)
- [ ] **Fixar mensagem** (24 h/7 d/sempre) e **faixa no topo** do chat.
  `PinInChatMessage`, `MessageAddOnDurationInSecs`,
  `WebMessageInfo.GetPinInChat()`. (M)
- [ ] **Dados da mensagem** (quem recebeu e leu, com horário) e quem
  reagiu. `events.Receipt` por participante; `GetUserReceipt()` no
  histórico. (M)
- [ ] **Seleção múltipla** com `shift+↑/↓`: encaminhar, apagar, copiar no
  formato `[14:03] Fulano: texto`. (M)
- [ ] **Encaminhar para até 5 conversas** (espaço marca no seletor). (P)
- [ ] **Salvar na conversa** (mensagem temporária que não some).
  `KeepInChatMessage`. (M)
- [ ] **Apagar como admin** a mensagem de um membro. `cli.BuildRevoke`
  com o remetente. (P)

### Menu do chat (`alt+m`)
- [ ] **Marcar como não lida.** `appstate.BuildMarkChatAsRead(…, false, …)`. (P)
- [ ] **Mensagens temporárias** (24h/7d/90d/desligado).
  `cli.SetDisappearingTimer`. (P)
- [ ] **Bloquear / desbloquear.** `cli.UpdateBlocklist`. (P)
- [ ] **Sair do grupo** (`cli.LeaveGroup`), **apagar conversa**
  (`appstate.BuildDeleteChat`), **limpar conversa** (patch em
  `IndexClearChat`). Pedem confirmação. (P / P / M)

### Perfil
- [ ] **`enter` num participante**: Conversar · (admin) Remover ·
  Promover. `cli.UpdateGroupParticipants`. (M)
- [ ] **Link de convite** (copiar, redefinir) e QR. `cli.GetGroupInviteLink`. (P)

### Painéis acima da caixa
- [ ] **Várias fotos de uma vez** com legenda (álbum). `AlbumMessage`. (M)
- [ ] **Enviar em HD** / tamanho original. (P)
- [ ] **Enviar como figurinha** a partir de imagem (WebP 512×512). (M)
- [ ] **Enviar visualização única.** `ViewOnceMessageV2`. (P)
- [ ] **Arrastar arquivo para o kitty**: caminho colado vira anexo. (P)
- [ ] **Prévia de link ao enviar** (busca og:tags; opcional no TOML). (M)
- [ ] **Mandar GIF** em `alt+g` (busca via Giphy/Tenor, precisa de chave). (M)
- [ ] **@todos** no autocompletar (campos `NonJIDMentions`/`GroupMentions`
  a confirmar). (?)
- [ ] **Abrir link de convite `chat.whatsapp.com`**: mostra o grupo e
  oferece "Entrar". `cli.GetGroupInfoFromLink`, `cli.JoinGroupWithLink`. (P)

### Canais
- [ ] **Reagir** (`cli.NewsletterSendReaction`), **marcar visto**
  (`cli.NewsletterMarkViewed`), **deixar de seguir / silenciar**
  (`cli.UnfollowNewsletter`, `cli.NewsletterToggleMute`). (P)

## 3. Um dia

- [ ] **Criar enquete, evento, mandar contato** com comandos `/enquete`,
  `/evento`, `/contato` — depende da decisão 4.
  `cli.BuildPollCreation`, `EventMessage` à mão, `ContactMessage`. (M)
- [ ] **Responder evento** (Vou/Talvez/Não vou). Só pela API interna
  (`DangerousInternals().EncryptMsgSecret`). (M–G)
- [ ] **Criar grupo** (`cli.CreateGroup`) e **administrar**: permissões
  (`SetGroupAnnounce`, `SetGroupLocked`, `SetGroupMemberAddMode`), pedidos
  pendentes (`GetGroupRequestParticipants`), nome, descrição e foto. (M)
- [ ] **Status**: ver e responder (hoje descartado em `daemon.go`),
  postar (`SendMessage` para `StatusBroadcastJID`). (M–G)
- [ ] **Comunidades**: agrupar grupos e Avisos na lista
  (`cli.GetSubGroups`). (M)
- [ ] **Canais**: diretório e busca (só por API interna, frágil), criar
  canal (`cli.CreateNewsletter`). (G)
- [ ] **Busca por data e por tipo** (`data:12/10`, `tipo:pdf`) e **central
  de mídia** de todas as conversas. (P–M)
- [ ] **Tela "Eu"**: privacidade da conta (`cli.SetPrivacySetting`), nome
  (`appstate.BuildSettingPushName`), recado (`cli.SetStatusMessage`). (M)
- [ ] **Login por código do celular** em vez do QR. `cli.PairPhone`. (P)
- [ ] **Pedir de novo mídia antiga** ao celular (tira o "não pode ser
  baixada"). `cli.SendMediaRetryReceipt`. (M)
- [ ] **Filtro "Favoritas" e listas personalizadas** do celular.
  `events.LabelEdit`, `events.LabelAssociationChat`. (M)
- [ ] **Aviso de "conversa restrita"** (o dono bloqueou aparelhos
  conectados). `Conversation.GetLimitSharing()`. (P)
- [ ] **Transcrição de áudio local** (whisper.cpp, opcional). (G)
- [ ] **Tecla Enter configurável** e **sons de notificação** no TOML. (P)

## Fora do escopo

- **Chamadas de voz/vídeo**, compartilhar tela, sala de espera,
  transferir chamada: o whatsmeow só tem a sinalização, não a mídia.
- **Abrir visualização única**: o WhatsApp não entrega a aparelho
  conectado.
- **Denunciar**: não existe no whatsmeow.
- **Nome de usuário (@username)**: sem API ainda.
- **Remover aparelhos conectados, configurações estritas da conta**: só
  no celular.
- **Câmera, editor de foto/vídeo, painel de emoji em grade, menu de
  formatação**: pedem interface gráfica (o `:emoji` e a sintaxe `*…*`
  cobrem).
- **Temas de conversa, papel de parede, zoom**: o tema e o zoom são do
  terminal.
- **Meta AI, resumo e tradução por IA**, bloqueio do app com senha (o
  swaylock faz), várias contas, download em lote.

## Decisões pendentes (antes da leva 2)

1. **Botão direito na mensagem abre o menu?** Hoje responde direto; com o
   menu, responder vira dois cliques.
2. **`ctrl+f` vira busca dentro da conversa?** Hoje só repete o `ctrl+p`.
3. **`alt+d` vira escolha "para mim / para todos"?** Troca o "apertar duas
   vezes" de hoje.
4. **Comandos com `/`?** Mensagem que começa com `/` abre a lista; `esc`
   fecha e manda como está.

## Atalhos livres (para não colidir)

Livres e bons: `alt+. alt+r alt+s alt+g alt+v alt+n alt+z alt+↑ alt+↓
shift+↑ shift+↓ ctrl+↑`. Também: `alt+j alt+k alt+o alt+q alt+t alt+x
alt+y ctrl+g ctrl+l ctrl+y`.

Evitar: as teclas de edição da caixa de texto (`ctrl+a b d h k u w`,
`alt+b u l`, `alt+←/→`), `ctrl+shift+*` e `ctrl+tab` (o kitty pega),
`ctrl+z` (suspende o programa).
