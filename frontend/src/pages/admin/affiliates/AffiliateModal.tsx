import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { affiliatesApi, leadsApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { bpsToInput, centsToInput, formatMoney, formatPhoneInput, parseMoney, parsePercentBps } from '@/services/format';
import { partnerUrl, referralCode, referralUrl } from '@/services/referral';
import type { Affiliate, AffiliateInput, CommissionType, EquipmentModes, InstallPrices, KindPrices } from '@/types';

import company from '../company/Company.module.css';
import styles from './Affiliates.module.css';

interface Draft {
  name: string;
  handle: string;
  code: string;
  commissionType: CommissionType;
  commission: string;
  percent: string;
  /** A mensalidade pelo link por tipo de veículo (falso: o preço normal do site). */
  planByKind: boolean;
  planMotorcycle: string;
  planCar: string;
  planTruck: string;
  /** A instalação e a desinstalação pelo link ('': as da Farbo). */
  installMode: '' | InstallPrices['mode'];
  installPrice: string;
  uninstallPrice: string;
  /** A garantia de troca do comodato (vazio: a da Farbo). */
  guarantee: string;
  /** A página do afiliado pede senha; password: a nova (vazia mantém a que já existe). */
  passwordOn: boolean;
  password: string;
  equipmentModes: EquipmentModes;
  /** O representante cuida das instalações dos clientes dele (pede a senha da página). */
  manageInstalls: boolean;
  /** Ele entrega o rastreador em mãos (com os kits dele), sem frete. */
  handDelivery: boolean;
  /** Na landing como representante oficial: a cidade, a região e o WhatsApp público. */
  listed: boolean;
  listingCity: string;
  listingArea: string;
  listingWhatsapp: string;
  email: string;
  phone: string;
  pixKey: string;
  notes: string;
  active: boolean;
}

function draftOf(a: Affiliate | null, defaultCents: number): Draft {
  return {
    name: a?.name ?? '',
    handle: a?.handle ?? '',
    code: a?.code ?? '',
    commissionType: a?.commissionType ?? 'fixed',
    commission: centsToInput(a?.commissionCents ?? defaultCents),
    percent: a && a.commissionBps > 0 ? bpsToInput(a.commissionBps) : '',
    planByKind: !!a?.planPrices,
    planMotorcycle: a?.planPrices ? centsToInput(a.planPrices.motorcycleCents) : '',
    planCar: a?.planPrices ? centsToInput(a.planPrices.carCents) : '',
    planTruck: a?.planPrices ? centsToInput(a.planPrices.truckCents) : '',
    installMode: a?.installPrices?.mode ?? '',
    installPrice: a?.installPrices ? centsToInput(a.installPrices.installCents) : '',
    uninstallPrice: a?.installPrices ? centsToInput(a.installPrices.uninstallCents) : '',
    guarantee: a?.installPrices?.guaranteeCents != null ? centsToInput(a.installPrices.guaranteeCents) : '',
    passwordOn: a?.reportPassword ?? false,
    password: '',
    equipmentModes: a?.equipmentModes ?? '',
    manageInstalls: a?.manageInstalls ?? false,
    handDelivery: a?.handDelivery ?? false,
    listed: a?.listed ?? false,
    listingCity: a?.listingCity ?? '',
    listingArea: a?.listingArea ?? '',
    listingWhatsapp: a?.listingWhatsapp ? formatPhoneInput(a.listingWhatsapp) : '',
    email: a?.email ?? '',
    phone: a?.phone ?? '',
    pixKey: a?.pixKey ?? '',
    notes: a?.notes ?? '',
    active: a?.active ?? true,
  };
}

/** O código que o servidor vai usar: o digitado, ou o do @, ou o do nome. */
export function codeFor(d: Pick<Draft, 'code' | 'handle' | 'name'>): string {
  return referralCode(d.code.trim() || d.handle.replace(/^@/, '') || d.name);
}

/**
 * Cadastro do afiliado e, depois de criado, os links: o de cadastro (com o
 * QR Code) e o da página dele com os números.
 */
export function AffiliateModal({
  affiliate,
  open,
  defaultCents,
  onClose,
  onSaved,
}: {
  /** null: novo afiliado. */
  affiliate: Affiliate | null;
  open: boolean;
  defaultCents: number;
  onClose: () => void;
  onSaved: (a: Affiliate) => void;
}) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Draft>(() => draftOf(affiliate, defaultCents));
  const [error, setError] = useState('');
  const [renewing, setRenewing] = useState(false);

  useEffect(() => {
    if (open) {
      setDraft(draftOf(affiliate, defaultCents));
      setError('');
      setRenewing(false);
    }
    // Só ao abrir (ou ao trocar de afiliado): salvar não apaga o que está na tela.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, affiliate?.id]);

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((d) => ({ ...d, [key]: value }));
  const code = codeFor(draft);

  const save = useMutation({
    mutationFn: (input: AffiliateInput) =>
      affiliate ? affiliatesApi.update(affiliate.id, input) : affiliatesApi.create(input),
    onSuccess: (saved) => {
      queryClient.invalidateQueries({ queryKey: ['affiliates'] });
      notify({
        tone: 'success',
        title: affiliate ? 'Afiliado atualizado' : 'Afiliado criado',
        description: affiliate ? undefined : 'Copie o link de cadastro e o da página dele para mandar ao afiliado.',
      });
      onSaved(saved);
    },
    onError: (err: Error) => setError(err.message),
  });

  const renew = useMutation({
    mutationFn: () => affiliatesApi.renewReportLink(affiliate!.id),
    onSuccess: (saved) => {
      queryClient.invalidateQueries({ queryKey: ['affiliates'] });
      notify({ tone: 'success', title: 'Link da página trocado', description: 'Mande o link novo ao afiliado: o antigo não abre mais.' });
      setRenewing(false);
      onSaved(saved);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível trocar o link', description: err.message }),
  });

  const submit = () => {
    setError('');
    const percent = draft.commissionType === 'percent';
    const cents = percent || draft.commission.trim() === '' ? null : parseMoney(draft.commission);
    if (!percent && draft.commission.trim() !== '' && cents === null) {
      setError('Valor por veículo inválido.');
      return;
    }
    const bps = percent ? parsePercentBps(draft.percent) : null;
    if (percent && (bps === null || bps <= 0 || bps > 10_000)) {
      setError('Percentual inválido: de 0,01 a 100 (por exemplo, 10 ou 12,5).');
      return;
    }
    let planPrices: KindPrices | null = null;
    if (draft.planByKind) {
      const [motorcycleCents, carCents, truckCents] = [draft.planMotorcycle, draft.planCar, draft.planTruck].map(parseMoney);
      if (!motorcycleCents || !carCents || !truckCents) {
        setError('Preencha a mensalidade da moto, do carro e do caminhão (por exemplo, 49,90).');
        return;
      }
      planPrices = { motorcycleCents, carCents, truckCents };
    }
    let installPrices: InstallPrices | null = null;
    if (draft.installMode) {
      const [installCents, uninstallCents] = [draft.installPrice, draft.uninstallPrice].map(parseMoney);
      if (installCents === null || uninstallCents === null) {
        setError('Preencha o preço da instalação e o da desinstalação (0 para sem custo).');
        return;
      }
      const guaranteeCents = draft.guarantee.trim() === '' ? null : parseMoney(draft.guarantee);
      if (draft.guarantee.trim() !== '' && guaranteeCents === null) {
        setError('Garantia de troca inválida: deixe em branco para a da Farbo (ou 0 para sem garantia).');
        return;
      }
      installPrices = { mode: draft.installMode, installCents, uninstallCents, guaranteeCents };
    }
    if (draft.listed && !draft.listingCity.trim()) {
      setError('Para aparecer na landing, diga a cidade do representante (por exemplo, Campinas/SP).');
      return;
    }
    if (draft.manageInstalls && !draft.passwordOn) {
      setError('Para o representante cuidar das instalações, marque a senha da página: ela mostra os dados dos clientes.');
      return;
    }
    // A senha da página: nova (ou trocada), tirada, ou a mesma (sem mandar).
    const hadPassword = affiliate?.reportPassword ?? false;
    let reportPassword: string | undefined;
    if (draft.passwordOn && draft.password !== '') {
      if (draft.password.length < 8) {
        setError('A senha da página precisa de pelo menos 8 caracteres.');
        return;
      }
      reportPassword = draft.password;
    } else if (draft.passwordOn && !hadPassword) {
      setError('Defina a senha da página (pelo menos 8 caracteres) ou desmarque a opção.');
      return;
    } else if (!draft.passwordOn && hadPassword) {
      reportPassword = '';
    }
    save.mutate({
      name: draft.name,
      handle: draft.handle,
      code: draft.code,
      commissionType: draft.commissionType,
      commissionCents: cents,
      commissionBps: bps,
      planPrices,
      installPrices,
      ...(reportPassword !== undefined ? { reportPassword } : {}),
      equipmentModes: draft.equipmentModes,
      manageInstalls: draft.manageInstalls,
      handDelivery: draft.handDelivery,
      listed: draft.listed,
      listingCity: draft.listingCity,
      listingArea: draft.listingArea,
      listingWhatsapp: draft.listingWhatsapp,
      email: draft.email,
      phone: draft.phone,
      pixKey: draft.pixKey,
      notes: draft.notes,
      active: draft.active,
    });
  };

  const copy = async (text: string, what: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: `${what} copiado`, description: text });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: text });
    }
  };

  const qr = (format: 'svg' | 'png') =>
    leadsApi
      .downloadQr(referralUrl(affiliate!.code), `indicacao-${affiliate!.code}`, format)
      .catch((err: Error) => notify({ tone: 'error', title: 'Não foi possível baixar', description: err.message }));

  const codeChanged = affiliate !== null && code !== affiliate.code;

  return (
    <Modal
      open={open}
      wide
      title={affiliate ? affiliate.name : 'Novo afiliado'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {affiliate ? 'Fechar' : 'Cancelar'}
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!draft.name.trim()} onClick={submit}>
            {affiliate ? 'Salvar' : 'Criar afiliado'}
          </Button>
        </>
      }
    >
      <div className={styles.modal}>
        {affiliate && (
          <section className={styles.links} aria-label="Links do afiliado">
            <div className={styles.linkBox}>
              <span className={styles.linkLabel}>Link de cadastro (para divulgar)</span>
              <code className={styles.linkText}>{referralUrl(affiliate.code)}</code>
              <div className={styles.linkButtons}>
                <Button size="small" variant="secondary" onClick={() => void copy(referralUrl(affiliate.code), 'Link de cadastro')}>
                  Copiar link
                </Button>
                <Button size="small" variant="ghost" onClick={() => void qr('png')}>
                  QR Code (PNG)
                </Button>
                <Button size="small" variant="ghost" onClick={() => void qr('svg')}>
                  QR Code (SVG)
                </Button>
                <a
                  className={styles.open}
                  href={`${partnerUrl(affiliate.reportToken)}/flyer`}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Gerar flyer
                </a>
              </div>
            </div>
            <div className={styles.linkBox}>
              <span className={styles.linkLabel}>Página do afiliado (só para ele: mostra os números)</span>
              <code className={styles.linkText}>{partnerUrl(affiliate.reportToken)}</code>
              <div className={styles.linkButtons}>
                <Button size="small" variant="secondary" onClick={() => void copy(partnerUrl(affiliate.reportToken), 'Link da página')}>
                  Copiar link
                </Button>
                <a className={styles.open} href={partnerUrl(affiliate.reportToken)} target="_blank" rel="noopener noreferrer">
                  Abrir
                </a>
                {renewing ? (
                  <span className={styles.confirm}>
                    O link atual para de abrir.
                    <Button size="small" variant="danger" loading={renew.isPending} onClick={() => renew.mutate()}>
                      Trocar
                    </Button>
                    <Button size="small" variant="ghost" onClick={() => setRenewing(false)}>
                      Não
                    </Button>
                  </span>
                ) : (
                  <Button size="small" variant="ghost" onClick={() => setRenewing(true)}>
                    Gerar link novo
                  </Button>
                )}
              </div>
            </div>
            <p className={company.summary}>
              <span>
                Cadastros pelo link <strong>{affiliate.signups}</strong>
              </span>
              <span>
                Clientes <strong>{affiliate.activeCustomers}</strong> ativos de <strong>{affiliate.customers}</strong>
              </span>
              <span>
                Veículos ativos <strong>{affiliate.activeVehicles}</strong>
              </span>
              <span>
                A fechar <strong>{formatMoney(affiliate.pendingCents)}</strong>
              </span>
              <span>
                A pagar <strong>{formatMoney(affiliate.openCents)}</strong>
              </span>
              <span>
                Pago <strong>{formatMoney(affiliate.paidCents)}</strong>
              </span>
            </p>
          </section>
        )}

        <div className={company.grid}>
          <TextField label="Nome" value={draft.name} maxLength={120} onChange={(e) => set('name', e.target.value)} autoFocus={!affiliate} />
          <TextField
            label="@ do Instagram"
            value={draft.handle}
            maxLength={31}
            placeholder="@fulano.moto"
            onChange={(e) => set('handle', e.target.value)}
            hint="Aparece na tela de cadastro: “Indicado por @fulano”."
          />
          <TextField
            label="Código do link"
            value={draft.code}
            maxLength={40}
            placeholder={codeFor({ ...draft, code: '' }) || 'sai do @ ou do nome'}
            onChange={(e) => set('code', e.target.value)}
            hint={
              codeChanged ? (
                <span className={company.danger}>Mudar o código muda o link: o antigo para de funcionar.</span>
              ) : (
                <>Link: {code ? referralUrl(code).replace(/^https:\/\//, '') : '—'}</>
              )
            }
          />
          <SelectField
            label="Comissão"
            value={draft.commissionType}
            onChange={(e) => set('commissionType', e.target.value as CommissionType)}
            hint="Por veículo do cliente indicado, em cada mês em que a mensalidade dele é paga."
          >
            <option value="fixed">Valor fixo por veículo</option>
            <option value="percent">Percentual da mensalidade</option>
          </SelectField>
          {draft.commissionType === 'percent' ? (
            <TextField
              label="Percentual da mensalidade (%)"
              value={draft.percent}
              inputMode="decimal"
              placeholder="10"
              onChange={(e) => set('percent', e.target.value)}
              hint={
                affiliate
                  ? 'O percentual novo vale para as mensalidades ainda não pagas. A parcela do rastreador fica fora.'
                  : 'Sobre a mensalidade paga de cada veículo; a parcela do rastreador fica fora.'
              }
            />
          ) : (
            <TextField
              label="Valor por veículo ativo, por mês (R$)"
              value={draft.commission}
              inputMode="decimal"
              onChange={(e) => set('commission', e.target.value)}
              hint={affiliate ? 'O valor novo vale para as mensalidades ainda não pagas.' : `Padrão: ${formatMoney(defaultCents)}.`}
            />
          )}
          <SelectField
            label="Mensalidade pelo link"
            value={draft.planByKind ? 'kind' : 'normal'}
            onChange={(e) => set('planByKind', e.target.value === 'kind')}
            hint={`${
              affiliate
                ? 'Vale para os veículos que ainda vão ser pedidos; as assinaturas que já existem ficam no preço delas.'
                : 'Quem contrata pelo link paga essa mensalidade, também nos próximos veículos (pelo app).'
            }${draft.planByKind ? ' Com o rastreador incluso, a parte dele soma nas primeiras mensalidades (Opções do pedido); depois, volta esta.' : ''}`}
          >
            <option value="normal">O preço normal do site</option>
            <option value="kind">Por tipo de veículo</option>
          </SelectField>
          {draft.planByKind && (
            <>
              <TextField
                label="Moto (R$/mês)"
                value={draft.planMotorcycle}
                inputMode="decimal"
                onChange={(e) => set('planMotorcycle', e.target.value)}
              />
              <TextField label="Carro (R$/mês)" value={draft.planCar} inputMode="decimal" onChange={(e) => set('planCar', e.target.value)} />
              <TextField
                label="Caminhão (R$/mês)"
                value={draft.planTruck}
                inputMode="decimal"
                onChange={(e) => set('planTruck', e.target.value)}
              />
            </>
          )}
          <SelectField
            label="Instalação e desinstalação pelo link"
            value={draft.installMode}
            onChange={(e) => set('installMode', e.target.value as Draft['installMode'])}
            hint={
              draft.installMode === 'direct'
                ? 'Ele instala, desinstala e recebe direto do cliente: os valores aparecem para o cliente, mas não entram no Pix nem nas faturas. Ele mesmo pode mudar os valores na página dele.'
                : draft.installMode === 'custom'
                  ? 'A Farbo cobra estes valores no lugar dos dela (a instalação no pedido, a desinstalação ao encerrar o comodato).'
                  : 'Quem contrata pelo link paga a instalação e a desinstalação da Farbo.'
            }
          >
            <option value="">As da Farbo</option>
            <option value="custom">Preços próprios (a Farbo cobra)</option>
            <option value="direct">Pelo representante (ele recebe direto)</option>
          </SelectField>
          {draft.installMode && (
            <>
              <TextField
                label="Instalação (R$)"
                value={draft.installPrice}
                inputMode="decimal"
                onChange={(e) => set('installPrice', e.target.value)}
              />
              <TextField
                label="Desinstalação do comodato (R$)"
                value={draft.uninstallPrice}
                inputMode="decimal"
                onChange={(e) => set('uninstallPrice', e.target.value)}
              />
              <TextField
                label="Garantia de troca no comodato (R$/mês)"
                value={draft.guarantee}
                inputMode="decimal"
                placeholder="a da Farbo"
                onChange={(e) => set('guarantee', e.target.value)}
                hint="Somada à mensalidade do comodato e cobrada pela Farbo. Em branco, a da Farbo; 0, sem garantia."
              />
            </>
          )}
          <SelectField
            label="Rastreador pelo link"
            value={draft.equipmentModes}
            onChange={(e) => set('equipmentModes', e.target.value as EquipmentModes)}
            hint="O que quem contrata pelo link pode escolher, também nos veículos que pedir depois pelo app."
          >
            <option value="">Incluso e comodato</option>
            <option value="sale">Só incluso (fica com o cliente)</option>
            <option value="loan">Só comodato</option>
          </SelectField>
          <TextField label="E-mail" type="email" value={draft.email} maxLength={254} onChange={(e) => set('email', e.target.value)} />
          <TextField
            label="WhatsApp"
            type="tel"
            value={draft.phone}
            maxLength={16}
            onChange={(e) => set('phone', formatPhoneInput(e.target.value, draft.phone))}
          />
          <TextField label="Chave Pix" value={draft.pixKey} maxLength={140} onChange={(e) => set('pixKey', e.target.value)} hint="Vai na conta a pagar do fechamento." />
        </div>
        <label className={company.check}>
          <input type="checkbox" checked={draft.active} onChange={(e) => set('active', e.target.checked)} />
          Ativo (o link indica e os meses pagos rendem comissão)
        </label>
        <label className={company.check}>
          <input type="checkbox" checked={draft.passwordOn} onChange={(e) => set('passwordOn', e.target.checked)} />
          Pedir senha para abrir a página do afiliado (os números e o cadastro para receber)
        </label>
        <label className={company.check}>
          <input
            type="checkbox"
            checked={draft.manageInstalls}
            onChange={(e) => {
              set('manageInstalls', e.target.checked);
              if (e.target.checked) set('passwordOn', true);
            }}
          />
          O representante cuida da instalação e da desinstalação dos clientes dele: agenda, remarca e conclui pela página
          dele (que então mostra os dados desses clientes e pede senha). A central continua vendo tudo na Agenda.
        </label>
        <label className={company.check}>
          <input type="checkbox" checked={draft.handDelivery} onChange={(e) => set('handDelivery', e.target.checked)} />
          Entrega em mãos pelo representante (ele recebe kits de instalação da Farbo): quem contrata pelo link dele, e os
          próximos pedidos pelo app, não paga frete.
        </label>
        <label className={company.check}>
          <input
            type="checkbox"
            checked={draft.listed}
            onChange={(e) => {
              set('listed', e.target.checked);
              // O WhatsApp do cadastro como sugestão: a central confere antes de publicar.
              if (e.target.checked && !draft.listingWhatsapp) set('listingWhatsapp', draft.phone);
            }}
          />
          Mostrar na landing como representante oficial autorizado (com a cidade, a região que atende, o WhatsApp abaixo e
          o botão para contratar pelo link dele). Só aparece enquanto estiver ativo.
        </label>
        {draft.listed && (
          <div className={company.grid}>
            <TextField
              label="Cidade (na landing)"
              value={draft.listingCity}
              maxLength={80}
              placeholder="Campinas/SP"
              onChange={(e) => set('listingCity', e.target.value)}
            />
            <TextField
              label="Região que atende (opcional)"
              value={draft.listingArea}
              maxLength={200}
              placeholder="Campinas, Valinhos e Vinhedo"
              onChange={(e) => set('listingArea', e.target.value)}
            />
            <TextField
              label="WhatsApp público (opcional)"
              type="tel"
              value={draft.listingWhatsapp}
              maxLength={16}
              onChange={(e) => set('listingWhatsapp', formatPhoneInput(e.target.value, draft.listingWhatsapp))}
              hint="Aparece na landing para qualquer pessoa. Em branco, sem o botão do WhatsApp."
            />
          </div>
        )}
        {draft.passwordOn && (
          <div className={company.grid}>
            <TextField
              label={affiliate?.reportPassword ? 'Nova senha da página' : 'Senha da página'}
              type="password"
              autoComplete="new-password"
              value={draft.password}
              maxLength={72}
              placeholder={affiliate?.reportPassword ? 'em branco: mantém a atual' : 'pelo menos 8 caracteres'}
              onChange={(e) => set('password', e.target.value)}
              hint="Passe a senha ao afiliado: a entrada vale 30 dias no aparelho dele. Trocar a senha (ou o link) pede a nova."
            />
          </div>
        )}
        <textarea
          className={company.textarea}
          aria-label="Observações"
          placeholder="Observações (combinado, contrato, contato do empresário...)"
          maxLength={2000}
          value={draft.notes}
          onChange={(e) => set('notes', e.target.value)}
        />
        {error && (
          <p className={company.danger} role="alert">
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}
