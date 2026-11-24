# Empacotamento

- `zapterm.desktop` e `zapterm.svg`: atalho e ícone do menu de aplicativos.
- `postinst.sh` e `postrm.sh`: só no .deb; criam e removem
  `/etc/apt/sources.list.d/zapterm.list`, que aponta o apt para o repositório
  https://arthurhmarinho.github.io/zapterm/deb (é por ele que o Gerenciador de
  Atualizações do Mint recebe as versões novas).
- `zapterm-archive-keyring.gpg`: chave pública que o apt usa para conferir o
  repositório (`signed-by`). Instalada em `/usr/share/keyrings/`.

## Gerar a chave do repositório (uma vez só)

```sh
# 1. chave só para assinar o repositório, sem senha (o CI assina sem terminal)
gpg --quick-gen-key "zapterm apt" ed25519 sign never

# 2. parte pública, em binário, para dentro do repositório
gpg --export "zapterm apt" > packaging/zapterm-archive-keyring.gpg

# 3. parte privada, para o secret APT_GPG_KEY (Settings → Secrets → Actions)
gpg --armor --export-secret-keys "zapterm apt"
```

Depois ative o GitHub Pages (Settings → Pages) na branch `gh-pages`, pasta
raiz. A branch é criada pelo primeiro release. Faça o commit do chaveiro
**antes** de criar a tag: o `release.yml` confere se o arquivo é uma chave de
verdade e para se ainda for este placeholder.
