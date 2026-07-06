# Instalar o WhatsApp no Claude (Windows) — passo a passo

Guia para quem **não é técnico**. No fim, você conversa com seu WhatsApp dentro do app do Claude.
Leva uns 5 minutos e você só precisa do **app do Claude** e do **seu celular**.

> Tudo roda no seu computador — suas mensagens não vão para a nuvem.

---

## Antes de começar
- Tenha o **aplicativo do Claude para Windows** instalado e logado (o app de desktop, não o site).
- Tenha o **celular com WhatsApp** por perto (para escanear um QR, como no WhatsApp Web).

---

## Passo 1 — Baixar o arquivo
Baixe o arquivo **`exed-whatsapp.mcpb`** no link interno da Exed:

> 🔗 **[COLOCAR AQUI o link do SharePoint/Teams]**

Guarde-o em algum lugar fácil, como a Área de Trabalho.

## Passo 2 — Instalar no Claude
1. Abra o app do **Claude**.
2. Vá em **Configurações (Settings) → Extensões (Extensions)**.
3. Clique em **"Install Extension…"** e escolha o arquivo `exed-whatsapp.mcpb` que você baixou.
4. Clique em **Instalar** e confirme.

> ⚠️ **Pode aparecer um aviso de segurança.** Como é uma ferramenta interna (não assinada por uma
> loja), o Windows pode mostrar **"O Windows protegeu o seu PC"**. Clique em **"Mais informações"**
> e depois em **"Executar assim mesmo"**. Se o Claude pedir para confirmar uma extensão "não
> verificada", confirme — é esperado.
>
> _[print: aviso do SmartScreen com o botão "Mais informações"]_
> _[print: tela de Extensões do Claude com "Install Extension…"]_

## Passo 3 — Reiniciar o Claude
Feche e abra o app do Claude de novo (ele só reconhece a extensão depois de reiniciar).

## Passo 4 — Conectar seu WhatsApp
Na conversa com o Claude, escreva:

> **conecte meu whatsapp**

Vai aparecer um **QR code** na conversa. No celular:
**WhatsApp → Configurações → Aparelhos conectados → Conectar um aparelho** → aponte para o QR.

> _[print: QR code aparecendo dentro da conversa do Claude]_

Pronto! Em alguns segundos ele conecta e começa a sincronizar suas conversas recentes.

## Passo 5 — Usar
Fale naturalmente com o Claude. Exemplos:
- **"o que chegou no meu zap hoje?"**
- **"resume a conversa com a Maria"**
- **"responde pro João que confirmo a reunião de amanhã"**

Ao responder, o Claude **mostra o rascunho** (para quem + o texto) e **só envia depois que você
aprovar**. Nada é enviado sem o seu OK.

---

## Perguntas rápidas

**Preciso deixar algo aberto?** Só o app do Claude. Quando ele está aberto, seu WhatsApp sincroniza;
quando está fechado, ele apenas não recebe mensagens novas naquele período (pega o recente quando
você abre de novo).

**De quanto em quanto tempo preciso reconectar?** A cada ~20 dias o WhatsApp desvincula o aparelho
(igual ao WhatsApp Web). Quando isso acontecer, é só pedir **"conecte meu whatsapp"** de novo e
escanear o QR.

**É seguro?** As mensagens ficam só no seu computador. A conexão usa a mesma tecnologia do WhatsApp
Web. Por segurança, o Claude nunca envia mensagem sem a sua confirmação e não dispara mensagens em
massa.

**Deu erro / não conectou?** Feche e abra o Claude, peça "conecte meu whatsapp" de novo. Se o QR
sumir antes de escanear, é só pedir de novo (ele expira rápido). Persistindo, chame o time interno.
