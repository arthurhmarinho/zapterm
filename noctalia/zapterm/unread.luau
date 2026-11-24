--!nonstrict
-- Widget de barra do zapterm. O daemon empurra o resumo de não lidas com
--   noctalia msg plugin arthurhmarinho/zapterm:unread all set '<json>'
-- e, ao carregar, o widget pergunta uma vez (zapterm unread --json) para não
-- ficar zerado até a próxima mensagem. Clique: zapterm open.

-- Padrão "zapterm": o que estiver no PATH; se o Noctalia não o enxergar lá,
-- cai para ~/go/bin/zapterm. Um caminho escolhido nas opções vale como está.
local configured = noctalia.getConfig("zapterm_path")
local zapterm
if configured == nil or configured == "" or configured == "zapterm" then
  zapterm = [[sh -c 'exec "$(command -v zapterm || echo "$HOME/go/bin/zapterm")" "$@"' zapterm]]
else
  zapterm = noctalia.expandPath(configured)
end
local hideWhenZero = noctalia.getConfig("hide_when_zero")

local summary = { chats = 0, messages = 0, top = {} }
local online = false

local function render()
  local n = summary.chats or 0
  barWidget.setGlyph("message-circle")
  if not online then
    barWidget.setText("")
    barWidget.setGlyphColor("on_surface_variant")
    barWidget.setTooltip("zapterm: daemon desligado")
    barWidget.setVisible(not hideWhenZero)
    return
  end
  if n == 0 then
    barWidget.setText("")
    barWidget.setGlyphColor("on_surface")
    barWidget.setTooltip("zapterm: nada não lido")
    barWidget.setVisible(not hideWhenZero)
    return
  end
  barWidget.setVisible(true)
  barWidget.setText(tostring(n))
  barWidget.setGlyphColor("primary")
  local lines = { `zapterm: {n} conversas, {summary.messages or 0} mensagens` }
  for _, c in ipairs(summary.top or {}) do
    table.insert(lines, `{c.unread}  {c.name}`)
  end
  barWidget.setTooltip(table.concat(lines, "\n"))
end

local function apply(json)
  local ok, data = pcall(noctalia.json.decode, json)
  if ok and type(data) == "table" then
    summary = data
    online = true
  end
  render()
end

local function refresh()
  noctalia.runAsync(`{zapterm} unread --json`, function(result)
    if type(result) == "table" and not result.timedOut and result.exitCode == 0 then
      apply(result.stdout)
    else
      online = false
      render()
    end
  end, 5000)
end

-- Ao carregar, e de vez em quando como rede de segurança (o normal é o
-- daemon empurrar pelo onIpc).
function update()
  noctalia.setUpdateInterval(300000)
  refresh()
end

function onClick()
  noctalia.runAsync(`{zapterm} open`, nil)
end

function onIpc(event, payload)
  if event == "set" then
    apply(payload)
  end
end

render()
