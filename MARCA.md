# Política de marca e identidade visual

O código deste repositório é aberto: está sob a [Apache License 2.0](LICENSE) e pode ser usado, estudado, modificado e redistribuído, inclusive para fins comerciais. **A marca não.** O nome, os logotipos e a identidade visual da Farbo Rastreadores são de uso exclusivo da empresa e **não fazem parte da licença**. A seção 6 da Apache 2.0 já exclui marcas e nomes comerciais; este documento detalha o que isso significa na prática.

## O que é livre

Todo o código-fonte: backend, frontend, migrations, scripts, configurações e documentação técnica. Os termos são os da Apache 2.0 (manter o `LICENSE` e o `NOTICE`, indicar as alterações feitas). Você pode, por exemplo, rodar a plataforma na sua empresa, adaptá-la a outros rastreadores ou oferecer um serviço baseado nela, **com o seu próprio nome e a sua própria identidade visual**.

## O que é reservado à Farbo Rastreadores

- **Nomes.** Cobre:
  - "FARBO RASTREADORES" e "FARBO RASTREAMENTO";
  - "Farbo" como nome de produto, serviço, aplicativo ou empresa de rastreamento;
  - qualquer variação que possa ser confundida com eles: grafias parecidas, traduções, siglas, domínios, perfis em redes sociais e nomes de aplicativo.
- **Logotipos e imagens de marca.** Cobre:
  - as imagens em `frontend/public/assets/`, entre elas `logo-header.png` e `logo-mark.png`;
  - os ícones do app do cliente em `frontend/public/app/icons/`, gerados da marca;
  - qualquer outra representação gráfica do nome ou do símbolo.

  As imagens desse diretório são material de marca e divulgação, não código: não são licenciadas pela Apache 2.0.
- **Identidade visual.** Cobre a aparência que identifica o produto:

  | Cor | Uso |
  | --- | --- |
  | `#3BE558` e `#4AF067` | verde neon, cor da marca |
  | `#15803D` | verde da marca no tema claro |
  | `#060907` | preto esverdeado de fundo |
  | `#040705`, `#0D130E`, `#121A14` e `#17211A` | superfícies da mesma família |

  Também é reservada a combinação de verde neon sobre fundo quase preto usada no painel, na landing, nos e-mails e nos ícones do mapa.
- **Contatos e presença da marca:** endereços de e-mail, telefone/WhatsApp, redes sociais (como `@farborastreadores`), textos comerciais e slogans que apresentam a Farbo.

## Ao reutilizar o código: o que trocar

Antes de publicar ou distribuir um produto feito a partir deste código:

1. **Nome.** Troque o nome em:
   - `frontend/index.html` (título, descrição e metatags);
   - na landing, em `frontend/src/components/landing/` (cabeçalho, rodapé, textos, contatos e redes sociais);
   - nos textos alternativos da logo em `frontend/src/components/layout/`;
   - nos modelos de e-mail em `backend/internal/mail/`;
   - no remetente (`MAIL_FROM`) e no README.
2. **Logotipos e imagens.** Substitua todas as imagens de `frontend/public/assets/` e gere de novo os ícones do app (`frontend/public/app/icons/`, com `node scripts/pwa-icons.mjs` a partir da sua logo). No manifesto do app (`frontend/public/app/manifest.webmanifest`), troque o nome e as cores.
3. **Cores.** Troque a paleta em:
   - `frontend/src/styles/tokens.css`: `--accent*`, `--success*`, `--surface-*`, `--border-*` e `--text-inverse`;
   - as cores fixas na landing: arquivos `*.module.css` em `frontend/src/components/landing/` e `frontend/src/pages/LandingPage.module.css`;
   - os ícones do mapa: `frontend/src/components/map/markers.ts` e `TrackerMap.tsx`;
   - os e-mails: `backend/internal/mail/account.go` e `shipment.go`;
   - o `theme-color` em `frontend/index.html`.
4. **Sem sugerir ligação com a Farbo.** O produto derivado não pode:
   - usar "Farbo" no nome, no domínio, no aplicativo ou nas redes sociais;
   - apresentar-se como versão oficial, parceiro ou revenda da Farbo Rastreadores.

Nomes técnicos internos que o usuário final não vê podem ficar, como o caminho do módulo Go (`github.com/pedrofarbo/farbo-rastreamento/...`) e prefixos internos de chaves e funções. Num fork, o caminho do módulo naturalmente passa a ser o do seu repositório.

## Usos permitidos sem pedir autorização

- **Informar a origem do código**, de forma factual e sem destaque que sugira endosso. Pode ser em créditos, na documentação ou no seu `NOTICE`. Por exemplo: "baseado no código aberto do projeto Farbo Rastreadores".
- **Criar links** para este repositório.
- **Contribuir** com este projeto: issues e pull requests.
- **Citar o nome** em textos jornalísticos, acadêmicos ou comparativos, para se referir à própria Farbo Rastreadores.

Qualquer outro uso do nome, dos logotipos ou da identidade visual depende de autorização prévia e por escrito da Farbo Rastreadores. Para pedir, abra uma issue neste repositório ou use os canais oficiais de contato da empresa.

## Marcas de terceiros

Nomes e marcas de terceiros citados no código, na documentação ou nas imagens pertencem aos respectivos titulares. Aparecem aqui só para identificar integrações e equipamentos compatíveis; isso não implica licença nem endosso. Alguns exemplos:
- Melhor Envios, AbacatePay e OpenStreetMap;
- Insanos MC (o escudo em `insanos-escudo.webp`);
- fabricantes e modelos de rastreadores (GT06, J16, H02, TKSTAR).

---

## English summary

The source code is open source under the [Apache License 2.0](LICENSE) and may be used, modified and redistributed, including commercially.

The following belong exclusively to Farbo Rastreadores and are **not** licensed:
- the names **FARBO RASTREADORES**, **FARBO RASTREAMENTO** and "Farbo" as a product or service name;
- the logos and the images in `frontend/public/assets/`;
- the visual identity: brand colors `#3BE558`, `#4AF067`, `#15803D`, the dark green-black surfaces `#060907`, `#040705`, `#0D130E`, `#121A14`, `#17211A`, and the neon-green-on-near-black look.

Before distributing or deploying a derived product, replace the name, the logos and the color palette, and do not suggest any affiliation with Farbo Rastreadores. A factual statement such as "based on the open-source Farbo Rastreadores project" is allowed.
