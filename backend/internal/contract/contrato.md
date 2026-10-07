# Contrato de Prestação de Serviços de Rastreamento Veicular

Termo de adesão, versão {{.Version}}, em vigor desde {{.EffectiveDate}}.

## 1. As partes

CONTRATADA: {{.LegalName}} ("{{.Name}}"), inscrita no CNPJ sob o nº {{.CNPJ}}{{if .Address}}, com sede em {{.Address}}{{end}}.

CONTRATANTE: a pessoa identificada no aceite eletrônico deste contrato pelo nome, pelo CPF (ou CNPJ) e pelo e-mail da conta ("Cliente").

## 2. O que é contratado

O serviço contratado é exclusivamente o sistema de rastreamento remoto: a plataforma (painel na web e app no celular), por assinatura mensal por veículo, com a localização em tempo real, o histórico de trajetos, os alertas, as cercas virtuais, o modo roubo, o acesso de pessoas autorizadas pelo Cliente e o bloqueio e o desbloqueio remotos do motor (com o relé instalado), comandados pelo próprio Cliente.

O Cliente pode dar a terceiros, cada um com a própria conta, acesso aos veículos dele: essas pessoas acompanham a posição ao vivo e, se o Cliente permitir, bloqueiam o motor numa emergência (nunca desbloqueiam). O Cliente escolhe quem tem acesso, pode retirá-lo a qualquer momento e responde por essas pessoas.

O serviço inclui a conexão do rastreador com a plataforma por um chip M2M (máquina a máquina) fornecido pela {{.Name}}.

Os Termos de Uso e a Política de Privacidade da plataforma fazem parte deste contrato. Se houver conflito, vale este contrato.

## 3. O que o serviço não é

A {{.Name}} não presta, e este contrato não inclui:

- serviços de segurança ou de vigilância;
- central de monitoramento;
- seguro veicular;
- equipe tática ou de pronta-resposta de prontidão.

A {{.Name}} não garante a recuperação do veículo em caso de roubo ou furto. O rastreamento é uma ferramenta para o próprio Cliente acompanhar o veículo e agir: em caso de roubo ou furto, é o Cliente quem aciona a polícia (190).

## 4. O rastreador é do Cliente

O rastreador comprado pelo Cliente é de propriedade exclusiva dele desde a entrega. Não é comodato, locação nem empréstimo: o equipamento continua sendo do Cliente depois do fim da assinatura, por qualquer motivo, e a {{.Name}} não pode exigir a sua devolução nem retê-lo por dívida.

O chip M2M instalado no rastreador é vinculado ao plano de telecomunicações da {{.Name}} junto à operadora e serve apenas para a comunicação do rastreador com a plataforma. Quando a assinatura termina, a linha do chip é desativada.

A instalação é feita por prestadores independentes indicados na plataforma, contratados e pagos diretamente pelo Cliente.

## 5. Preço, faturas e pagamento

- A mensalidade de cada veículo é a do plano escolhido no pedido, mostrada antes da confirmação. Condições promocionais valem pelo prazo informado na contratação.
- As faturas ficam disponíveis na plataforma antes do vencimento e são enviadas por e-mail, com o link para pagar por Pix.
{{- if gt .MaxInstallments 1}}
- O rastreador pode ser pago à vista ou parcelado em até {{.MaxInstallments}} ({{.MaxInstallmentsWords}}) vezes sem juros, por Pix: a 1ª parcela vence com o pedido, junto com o frete, e as demais vêm somadas às mensalidades seguintes daquele veículo, uma por mês. O número e o valor das parcelas são mostrados antes da confirmação do pedido.
{{- end}}
- Reajuste anual pelo IPCA: a mensalidade é reajustada uma vez por ano, em agosto, pela variação do IPCA (IBGE) acumulada nos 12 meses encerrados em maio daquele ano, o último índice publicado antes do aviso. O valor novo vale para as faturas que vencem a partir de 1º de agosto e é avisado por e-mail com pelo menos 30 dias de antecedência; se o aviso sair depois de 1º de julho, o reajuste passa a valer 30 dias depois dele.
- Só é reajustada a assinatura que completar 12 meses até a data do reajuste (Lei 10.192/2001); as mais novas são reajustadas no agosto seguinte. Se o índice acumulado for zero ou negativo, a mensalidade fica como está. Se o IPCA deixar de ser publicado, vale o índice oficial que o substituir.
- Outras mudanças no preço da mensalidade também são avisadas por e-mail com pelo menos 30 dias de antecedência. Nenhuma mudança, nem o reajuste, altera condições promocionais já contratadas durante o seu prazo.

## 6. Atraso no pagamento

- Com fatura vencida há mais de {{.SuspendAfterDays}} dias, o acesso à plataforma fica suspenso até o pagamento. O rastreador continua registrando as posições, que voltam a aparecer quando o acesso é liberado.
- Com atraso de mais de 1 (um) mês, o nome do Cliente poderá ser incluído nos órgãos de proteção ao crédito (como SPC e Serasa), sempre com comunicação prévia por escrito, como determina o art. 43, § 2º, do Código de Defesa do Consumidor. Depois do pagamento, a exclusão é pedida em até 5 dias úteis.
- Em atraso prolongado, a assinatura pode ser encerrada, com aviso prévio por e-mail. As faturas vencidas continuam devidas.

## 7. Permanência mínima e multa

- Cada assinatura tem permanência mínima de {{.MinMonths}} (três) meses, contados da data de início da assinatura do veículo.
- Se o Cliente encerrar a assinatura ou a conta antes de completar os {{.MinMonths}} meses, paga multa equivalente a 1 (uma) mensalidade do plano daquele veículo, além das faturas vencidas.
- A multa não se aplica: à desistência no prazo de arrependimento (cláusula 9); ao encerramento causado por falha da {{.Name}} na prestação do serviço; ao encerramento pela {{.Name}} sem culpa do Cliente; e à recusa de uma nova versão deste contrato (cláusula 14).
- Depois dos {{.MinMonths}} meses, o Cliente pode cancelar a qualquer momento, sem multa.
{{- if gt .MaxInstallments 1}}
- Quem parcela o rastreador mantém a assinatura daquele veículo ativa até a mensalidade que traz a última parcela, prazo informado no pedido. Se a assinatura for encerrada antes, a pedido do Cliente ou por atraso no pagamento, as parcelas restantes vencem de uma vez, numa fatura só. Elas não são multa: são o preço do rastreador, que já é do Cliente (cláusula 4). Na desistência dentro do prazo de arrependimento (cláusula 9), com a devolução do equipamento, as parcelas restantes não são cobradas.
{{- end}}

## 8. Por que existem a permanência mínima, a multa e a negativação

Para cada rastreador, a {{.Name}} contrata e mantém junto a uma operadora um plano de telecomunicações M2M (máquina a máquina) e arca com a geração, a habilitação e a entrega do chip. Esses custos não são cobrados à parte do Cliente: eles são diluídos nas mensalidades.

A permanência mínima, a multa e a possibilidade de inclusão nos órgãos de proteção ao crédito existem para cobrir esses custos quando a assinatura termina cedo ou não é paga.

## 9. Direito de arrependimento

O Cliente pode desistir em até 7 (sete) dias a contar do recebimento do rastreador, sem multa, como garante o art. 49 do Código de Defesa do Consumidor. A {{.Name}} devolve os valores pagos e combina com o Cliente a devolução do equipamento.

## 10. Cadastro, CPF e notas fiscais

- O CPF do Cliente (ou o CNPJ, se o Cliente for empresa) é obrigatório e deve estar correto: é com ele que a {{.Name}} emite a NF-e (nota fiscal eletrônica) do rastreador e a NFS-e (nota fiscal de serviço eletrônica) das mensalidades.
- O Cliente mantém atualizados o nome, o CPF, o telefone, o e-mail e o endereço de entrega.
- A conta é pessoal e só pode ser criada por maiores de 18 anos.

## 11. Uso do serviço e responsabilidades

- O rastreamento depende da cobertura da rede móvel, do sinal de GPS, da alimentação elétrica do veículo e da instalação correta do rastreador.
- Em caso de roubo ou furto, o Cliente aciona a polícia (190) e usa o modo roubo da plataforma (cláusula 3). Ninguém deve ir atrás do veículo.
- Por segurança, o bloqueio remoto do motor só é enviado com o veículo parado ou em velocidade muito baixa.
- O Cliente só rastreia veículos seus ou que esteja autorizado a rastrear, avisa os condutores habituais de que o veículo é rastreado e responde pelas pessoas que autoriza na plataforma. É proibido usar o serviço para vigiar ou perseguir pessoas sem o conhecimento delas (Lei nº 14.132/2021).
- O chip M2M só pode ser usado no rastreador: é proibido retirá-lo para qualquer outro uso.

## 12. Dados pessoais

A {{.Name}} trata os dados do Cliente, inclusive as posições do veículo, conforme a Lei Geral de Proteção de Dados (Lei nº 13.709/2018) e a Política de Privacidade. O histórico de posições é guardado pelo prazo escolhido na plataforma ({{.HistoryOptions}} dias).

## 13. Vigência e encerramento

- Este contrato vale por prazo indeterminado, respeitada a permanência mínima de cada assinatura.
- O Cliente pode encerrar uma assinatura ou a conta pelos canais de contato ({{.Email}}).
- A {{.Name}} pode encerrar a conta em caso de uso proibido ou de atraso prolongado no pagamento, com aviso prévio sempre que possível.
- Com o fim da assinatura, a linha do chip M2M é desativada e o rastreador continua sendo do Cliente.

## 14. Mudanças neste contrato

A {{.Name}} pode atualizar este contrato. As mudanças são avisadas por e-mail com pelo menos 30 dias de antecedência e a plataforma pede um novo aceite. Se o Cliente não concordar com a nova versão, pode encerrar a assinatura sem multa, mesmo dentro da permanência mínima.

## 15. Aceite eletrônico

O aceite eletrônico deste contrato, registrado com a data, a hora, o endereço IP, o navegador e a versão aceita, tem validade jurídica e equivale à assinatura, nos termos do art. 10, § 2º, da Medida Provisória nº 2.200-2/2001. Uma cópia é enviada ao e-mail do Cliente e fica disponível na plataforma.

## 16. Lei e foro

Este contrato segue a legislação brasileira. Fica eleito o foro do domicílio do Cliente, como prevê o Código de Defesa do Consumidor.
