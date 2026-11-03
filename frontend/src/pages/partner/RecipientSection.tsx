import { useState } from 'react';
import type { ReactNode } from 'react';

import { publicApi } from '@/api/resources';
import { parseMoney } from '@/services/format';
import { lookupZipCode } from '@/services/zipcode';
import type { KycLink, PartnerRecipient, RecipientInput } from '@/types';

import styles from '../Partner.module.css';
import base from '../PreLaunch.module.css';

/**
 * O recebimento pelo split, na página do afiliado: o cadastro de recebedor
 * na Pagar.me (os dados vão direto para ela; a Farbo guarda só a situação), a
 * prova de vida pelo QR Code dela e a situação. Ativo, a comissão cai na
 * conta dele no próprio pagamento do cliente.
 */
export function RecipientSection({
  token,
  recipient,
  onChange,
}: {
  token: string;
  recipient: PartnerRecipient;
  onChange: (r: PartnerRecipient) => void;
}) {
  const [kyc, setKyc] = useState<KycLink | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const kycLink = async () => {
    setBusy(true);
    setError('');
    try {
      setKyc(await publicApi.partnerKycLink(token));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Não deu para gerar o link agora.');
    } finally {
      setBusy(false);
    }
  };

  let body: ReactNode;
  switch (recipient.status) {
    case '':
      body = <RecipientForm token={token} onDone={onChange} />;
      break;
    case 'active':
      body = (
        <p className={styles.qrHint}>
          ✅ Cadastro ativo. A sua comissão sai no próprio pagamento do cliente, direto para a sua conta (a Pagar.me
          repassa todo dia). As comissões que estavam acumuladas vão junto nos próximos pagamentos.
        </p>
      );
      break;
    case 'refused':
      body = (
        <p className={base.error}>A Pagar.me recusou o cadastro. Fale com a Farbo para ver o que fazer.</p>
      );
      break;
    case 'affiliation':
      body = (
        <p className={styles.qrHint}>
          Cadastro e prova de vida enviados: a Pagar.me está analisando. Isso costuma levar até 2 dias úteis; esta página
          mostra quando ficar ativo.
        </p>
      );
      break;
    default:
      body = (
        <>
          <p className={styles.qrHint}>
            Cadastro enviado. Falta a <strong>prova de vida</strong>: abra o link no celular (ou leia o QR Code com a
            câmera) e siga os passos da Pagar.me, com o seu documento em mãos.
          </p>
          {kyc ? (
            <>
              {kyc.base64 && (
                <div className={styles.qrBox}>
                  <img
                    className={styles.qr}
                    src={kyc.base64.startsWith('data:') ? kyc.base64 : `data:image/png;base64,${kyc.base64}`}
                    width={220}
                    height={220}
                    alt="QR Code da prova de vida"
                  />
                </div>
              )}
              <a className={base.primary} href={kyc.url} target="_blank" rel="noopener noreferrer">
                Abrir a prova de vida
              </a>
              <p className={styles.qrHint}>O link vale 20 minutos. Se vencer, gere outro.</p>
            </>
          ) : (
            <button type="button" className={base.primary} disabled={busy} onClick={() => void kycLink()}>
              {busy ? 'Gerando…' : 'Fazer a prova de vida'}
            </button>
          )}
        </>
      );
  }

  return (
    <section className={styles.qrCard} aria-label="Receber pelo split">
      <h2 className={styles.heading}>Receba na hora, direto na sua conta</h2>
      {recipient.status === '' && (
        <p className={styles.qrHint}>
          A comissão sai no próprio pagamento do cliente, pela Pagar.me, sem esperar o fechamento do mês. Para isso,
          cadastre-se como recebedor: os dados vão direto para a Pagar.me, e a Farbo não guarda nenhum deles.
        </p>
      )}
      {body}
      {error && (
        <p className={base.error} role="alert">
          {error}
        </p>
      )}
    </section>
  );
}

type Draft = Omit<RecipientInput, 'monthlyIncomeCents' | 'annualRevenueCents'> & { income: string; revenue: string };

const EMPTY: Draft = {
  kind: 'individual', document: '', name: '', email: '', phone: '', birthdate: '', income: '', occupation: '',
  companyName: '', tradingName: '', revenue: '', zipCode: '', street: '', number: '', complement: '', district: '',
  city: '', state: '', referencePoint: '', bank: '', branch: '', branchDigit: '', account: '', accountDigit: '',
  accountType: 'checking', accountHolderName: '',
};

/** "12101995" → "12/10/1995" (o campo do nascimento). */
function formatBirthdate(text: string): string {
  const d = text.replace(/\D/g, '').slice(0, 8);
  if (d.length > 4) return `${d.slice(0, 2)}/${d.slice(2, 4)}/${d.slice(4)}`;
  if (d.length > 2) return `${d.slice(0, 2)}/${d.slice(2)}`;
  return d;
}

function RecipientForm({ token, onDone }: { token: string; onDone: (r: PartnerRecipient) => void }) {
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((d) => ({ ...d, [key]: value }));
  const person = draft.kind === 'individual';

  const fillZip = async (zip: string) => {
    const found = await lookupZipCode(zip);
    if (found.status === 'found') {
      setDraft((d) => ({
        ...d,
        street: d.street || found.address.street,
        district: d.district || found.address.district,
        city: d.city || found.address.city,
        state: d.state || found.address.state,
      }));
    }
  };

  const submit = async () => {
    setError('');
    const income = person ? parseMoney(draft.income) : 0;
    const revenue = person ? 0 : parseMoney(draft.revenue);
    if (person && !income) return setError('Informe a renda mensal (ex.: 3.000,00).');
    if (!person && !revenue) return setError('Informe o faturamento anual (ex.: 120.000,00).');
    const { income: _income, revenue: _revenue, ...rest } = draft;
    setBusy(true);
    try {
      onDone(
        await publicApi.partnerRecipient(token, {
          ...rest,
          monthlyIncomeCents: income ?? 0,
          annualRevenueCents: revenue ?? 0,
        }),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Não deu para enviar agora. Tente de novo.');
    } finally {
      setBusy(false);
    }
  };

  const input = (label: string, key: keyof Draft, props: Record<string, unknown> = {}) => (
    <label className={base.field}>
      {label}
      <input value={draft[key] as string} onChange={(e) => set(key, e.target.value as never)} {...props} />
    </label>
  );

  return (
    <div className={styles.recipientForm}>
      <div className={styles.kindToggle} role="radiogroup" aria-label="Tipo de cadastro">
        {(['individual', 'company'] as const).map((k) => (
          <button
            key={k}
            type="button"
            role="radio"
            aria-checked={draft.kind === k}
            className={draft.kind === k ? styles.kindActive : styles.kind}
            onClick={() => set('kind', k)}
          >
            {k === 'individual' ? 'Pessoa física (CPF)' : 'Empresa (CNPJ)'}
          </button>
        ))}
      </div>

      {input(person ? 'CPF' : 'CNPJ', 'document', { inputMode: 'numeric', autoComplete: 'off' })}
      {person ? (
        <>
          {input('Nome completo', 'name', { autoComplete: 'name' })}
          <div className={styles.row2}>
            <label className={base.field}>
              Nascimento
              <input
                inputMode="numeric"
                placeholder="DD/MM/AAAA"
                value={draft.birthdate}
                onChange={(e) => set('birthdate', formatBirthdate(e.target.value))}
              />
            </label>
            {input('Renda mensal (R$)', 'income', { inputMode: 'decimal', placeholder: '3.000,00' })}
          </div>
          {input('Profissão', 'occupation', { placeholder: 'Ex.: influenciador digital' })}
        </>
      ) : (
        <>
          {input('Razão social', 'companyName')}
          {input('Nome fantasia', 'tradingName')}
          {input('Faturamento anual (R$)', 'revenue', { inputMode: 'decimal', placeholder: '120.000,00' })}
        </>
      )}
      <div className={styles.row2}>
        {input('E-mail', 'email', { type: 'email', autoComplete: 'email' })}
        {input('Celular com DDD', 'phone', { type: 'tel', autoComplete: 'tel' })}
      </div>

      <h3 className={styles.subheading}>Endereço</h3>
      <label className={base.field}>
        CEP
        <input
          inputMode="numeric"
          autoComplete="postal-code"
          value={draft.zipCode}
          onChange={(e) => set('zipCode', e.target.value)}
          onBlur={(e) => void fillZip(e.target.value)}
        />
      </label>
      {input('Rua', 'street', { autoComplete: 'address-line1' })}
      <div className={styles.row2}>
        {input('Número', 'number')}
        {input('Complemento', 'complement')}
      </div>
      {input('Bairro', 'district')}
      <div className={styles.row2}>
        {input('Cidade', 'city', { autoComplete: 'address-level2' })}
        {input('UF', 'state', { maxLength: 2, autoComplete: 'address-level1' })}
      </div>

      <h3 className={styles.subheading}>Conta para receber (no seu {person ? 'CPF' : 'CNPJ'})</h3>
      {input('Código do banco', 'bank', { inputMode: 'numeric', maxLength: 3, placeholder: '260 (Nubank), 341 (Itaú), 001 (BB)…' })}
      <div className={styles.row2}>
        {input('Agência (sem dígito)', 'branch', { inputMode: 'numeric', maxLength: 4 })}
        {input('Dígito da agência', 'branchDigit', { maxLength: 1, placeholder: 'se tiver' })}
      </div>
      <div className={styles.row2}>
        {input('Conta (sem dígito)', 'account', { inputMode: 'numeric', maxLength: 13 })}
        {input('Dígito da conta', 'accountDigit', { maxLength: 2 })}
      </div>
      <div className={styles.kindToggle} role="radiogroup" aria-label="Tipo de conta">
        {(['checking', 'savings'] as const).map((k) => (
          <button
            key={k}
            type="button"
            role="radio"
            aria-checked={draft.accountType === k}
            className={draft.accountType === k ? styles.kindActive : styles.kind}
            onClick={() => set('accountType', k)}
          >
            {k === 'checking' ? 'Conta corrente' : 'Poupança'}
          </button>
        ))}
      </div>

      {error && (
        <p className={base.error} role="alert">
          {error}
        </p>
      )}
      <button type="button" className={base.primary} disabled={busy} onClick={() => void submit()}>
        {busy ? 'Enviando…' : 'Enviar o cadastro para a Pagar.me'}
      </button>
    </div>
  );
}
