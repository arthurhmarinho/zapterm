# Widget do zapterm para o Noctalia

Mostra na barra quantas conversas têm mensagem não lida (sem contar
arquivadas e silenciadas), com a lista na dica. O clique abre o zapterm,
ou traz a janela dele para frente.

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

- **Caminho do zapterm** (padrão `~/go/bin/zapterm`): o Noctalia pode não
  ter `~/go/bin` no PATH.
- **Esconder sem não lidas** (padrão: não).
