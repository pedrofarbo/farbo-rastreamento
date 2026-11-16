import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { useNavigate, useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { adhesionLinkApi, contractApi } from '@/api/resources';
import { AddressFields, EMPTY_ADDRESS, isAddressComplete } from '@/components/address/AddressFields';
import { ContractText } from '@/components/contract/ContractText';
import { SelectField, TextField } from '@/components/ui/Field';
import { formatDateTime, formatPhoneInput, isPhoneComplete } from '@/services/format';
import { formatTaxId, taxIdDigits, validTaxId } from '@/services/taxid';
import type { AdhesionInput, ContractDocument, InspectionVehicleType, PublicAdhesion, PublicAdhesionOrg } from '@/types';

import { Shell } from './InstallerPage';
import local from './InstallerPage.module.css';
import base from './PreLaunch.module.css';
import { useNoIndex } from './PublicLocation';
import page from './PublicInspection.module.css';
import styles from './PublicTheft.module.css';

const PLATE = /^[A-Z]{3}[0-9][A-Z0-9][0-9]{2}$/;
const EMAIL = /^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/;

/** O que falta no formulário de adesão (ou nada). */
export function adhesionProblem(d: AdhesionInput): string | null {
  if (d.name.trim().split(/\s+/).length < 2) return 'Informe o seu nome completo.';
  if (!validTaxId(d.document)) return 'CPF inválido: confira os números.';
  if (!EMAIL.test(d.email.trim())) return 'Informe um e-mail válido: é por ele que chega o acesso ao app.';
  if (!isPhoneComplete(d.phone)) return 'Informe o celular com o DDD.';
  if (!isAddressComplete(d.address)) return 'Complete o endereço onde o rastreador vai ser instalado.';
  if (!PLATE.test(d.plate.toUpperCase().replace(/[^A-Z0-9]/g, ''))) return 'Informe a placa do veículo (ABC1D23 ou ABC1234).';
  if (!d.model.trim()) return 'Informe o modelo do veículo.';
  return null;
}

const EMPTY: AdhesionInput = {
  name: '', document: '', email: '', phone: '', address: EMPTY_ADDRESS, vehicleType: 'car', plate: '', brand: '', model: '',
  year: null, color: '',
};

/**
 * O link de adesão da associação (/adesao/<link>), sem login: quem quer
 * entrar manda os dados, o endereço da instalação e o veículo; em seguida, a
 * vistoria de adesão e o termo, pelo link de acompanhamento.
 */
export function PublicAdhesionPage() {
  const { token = '' } = useParams();
  const navigate = useNavigate();
  useNoIndex('Adesão · Farbo Rastreadores');
  const [org, setOrg] = useState<PublicAdhesionOrg | null>(null);
  const [loadError, setLoadError] = useState('');
  const [draft, setDraft] = useState<AdhesionInput>(EMPTY);
  const [error, setError] = useState('');
  const [sending, setSending] = useState(false);
  const set = (patch: Partial<AdhesionInput>) => setDraft((d) => ({ ...d, ...patch }));

  useEffect(() => {
    adhesionLinkApi
      .org(token)
      .then(setOrg)
      .catch((err) =>
        setLoadError(err instanceof ApiError && err.status === 404 ? 'Link não encontrado: confira o endereço que você recebeu.' : 'Não deu para abrir agora. Tente de novo.'),
      );
  }, [token]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const problem = adhesionProblem(draft);
    if (problem) {
      setError(problem);
      return;
    }
    setSending(true);
    setError('');
    try {
      const out = await adhesionLinkApi.start(token, {
        ...draft, document: taxIdDigits(draft.document), plate: draft.plate.toUpperCase().replace(/[^A-Z0-9]/g, ''),
      });
      navigate(`/adesao/acompanhar/${out.followToken}`);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSending(false);
    }
  };

  if (loadError || !org) {
    return (
      <Shell label="Adesão">
        <section className={styles.card}>
          {loadError ? <p className={base.error}>{loadError}</p> : <p className={styles.muted}>Abrindo…</p>}
        </section>
      </Shell>
    );
  }
  if (!org.enabled) {
    return (
      <Shell label="Adesão">
        <section className={styles.card}>
          <h1 className={styles.title}>{org.organizationName}</h1>
          <p className={styles.text}>A adesão por este link não está aberta agora. Fale com a associação.</p>
        </section>
      </Shell>
    );
  }

  return (
    <Shell label="Adesão">
      <section className={styles.card}>
        <span className={styles.muted}>Adesão com rastreamento</span>
        <h1 className={styles.title}>{org.organizationName}</h1>
        <p className={styles.text}>
          Leva uns 15 minutos: os seus dados e os do veículo, a vistoria pelo celular (perto do veículo) e o aceite do termo. Depois, a{' '}
          {org.kind === 'insurer' ? 'seguradora' : 'associação'} confere e a instalação do rastreador é agendada com você.
        </p>
      </section>
      <form className={page.form} onSubmit={(e) => void submit(e)} noValidate>
        <section className={styles.card}>
          <h2 className={local.step}>
            <span className={local.number}>1</span> Seus dados
          </h2>
          <TextField label="Nome completo" autoComplete="name" autoCapitalize="words" maxLength={120} value={draft.name} onChange={(e) => set({ name: e.target.value })} />
          <TextField label="CPF" inputMode="numeric" value={draft.document} onChange={(e) => set({ document: formatTaxId(e.target.value) })} />
          <TextField label="E-mail" type="email" autoComplete="email" value={draft.email} hint="O acesso ao app chega por aqui." onChange={(e) => set({ email: e.target.value })} />
          <TextField label="Celular (WhatsApp)" inputMode="tel" autoComplete="tel" value={draft.phone} onChange={(e) => set({ phone: formatPhoneInput(e.target.value) })} />
        </section>
        <section className={styles.card}>
          <h2 className={local.step}>
            <span className={local.number}>2</span> Onde instalar
          </h2>
          <AddressFields value={draft.address} onChange={(address) => set({ address })} />
        </section>
        <section className={styles.card}>
          <h2 className={local.step}>
            <span className={local.number}>3</span> O veículo
          </h2>
          <SelectField label="Veículo" value={draft.vehicleType} onChange={(e) => set({ vehicleType: e.target.value as InspectionVehicleType })}>
            <option value="car">Carro, caminhonete ou caminhão</option>
            <option value="motorcycle">Moto</option>
          </SelectField>
          <TextField label="Placa" maxLength={8} placeholder="ABC1D23" value={draft.plate} onChange={(e) => set({ plate: e.target.value.toUpperCase() })} />
          <TextField label="Marca" autoCapitalize="words" maxLength={40} value={draft.brand} onChange={(e) => set({ brand: e.target.value })} />
          <TextField label="Modelo" autoCapitalize="words" maxLength={60} value={draft.model} onChange={(e) => set({ model: e.target.value })} />
          <TextField
            label="Ano"
            inputMode="numeric"
            maxLength={4}
            value={draft.year ?? ''}
            onChange={(e) => {
              const n = Number(e.target.value.replace(/\D/g, ''));
              set({ year: n > 0 ? n : null });
            }}
          />
          <TextField label="Cor" autoCapitalize="words" maxLength={30} value={draft.color} onChange={(e) => set({ color: e.target.value })} />
        </section>
        {error && <p className={base.error}>{error}</p>}
        <button type="submit" className={`${base.primary} ${local.action}`} disabled={sending}>
          {sending ? 'Enviando…' : 'Continuar para a vistoria'}
        </button>
      </form>
    </Shell>
  );
}

/** O estado de cada passo, para o número verde. */
function stepDone(done: boolean) {
  return done ? local.done : '';
}

/**
 * O acompanhamento da adesão (/adesao/acompanhar/<link>): a vistoria (o link
 * dela), o termo do associado com a preferência da instalação e a situação —
 * em análise, aprovada (o acesso ao app chega por e-mail) ou recusada, com o
 * motivo.
 */
export function PublicAdhesionFollowPage() {
  const { token = '' } = useParams();
  useNoIndex('Adesão · Farbo Rastreadores');
  const [view, setView] = useState<PublicAdhesion | null>(null);
  const [term, setTerm] = useState<ContractDocument | null>(null);
  const [loadError, setLoadError] = useState('');
  const [showTerm, setShowTerm] = useState(false);
  const [accept, setAccept] = useState(false);
  const [preference, setPreference] = useState('');
  const [error, setError] = useState('');
  const [sending, setSending] = useState(false);

  useEffect(() => {
    adhesionLinkApi
      .view(token)
      .then(setView)
      .catch((err) =>
        setLoadError(err instanceof ApiError && err.status === 404 ? 'Link não encontrado: confira o endereço que você recebeu.' : 'Não deu para abrir agora. Tente de novo.'),
      );
    contractApi.current(true).then(setTerm).catch(() => undefined);
  }, [token]);

  if (loadError || !view) {
    return (
      <Shell label="Adesão">
        <section className={styles.card}>
          {loadError ? <p className={base.error}>{loadError}</p> : <p className={styles.muted}>Abrindo…</p>}
        </section>
      </Shell>
    );
  }

  const inspectionSent = view.inspectionStatus === 'submitted' || view.inspectionStatus === 'approved';
  const acceptTerm = async () => {
    if (!term) return;
    setSending(true);
    setError('');
    try {
      setView(await adhesionLinkApi.accept(token, term.version, preference));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSending(false);
    }
  };

  return (
    <Shell label="Adesão">
      <section className={styles.card}>
        <span className={styles.muted}>Adesão · {view.organizationName}</span>
        <h1 className={styles.title}>{view.vehicleName}</h1>
        <span className={styles.plate}>{view.plate}</span>
        {view.firstName && <p className={styles.text}>Olá, {view.firstName}! Guarde este link para acompanhar a sua adesão.</p>}
      </section>

      {view.status === 'rejected' ? (
        <section className={styles.card}>
          <p className={styles.text}>
            A adesão não foi aprovada por {view.organizationName}.{view.reviewNote ? ` Motivo: ${view.reviewNote}` : ''}
          </p>
        </section>
      ) : view.status === 'approved' ? (
        <section className={`${styles.card} ${local.done}`}>
          <h2 className={local.step}>✅ Adesão aprovada</h2>
          <p className={styles.text}>
            Boas-vindas à {view.organizationName}! O acesso ao app chega no seu e-mail, e a instalação do rastreador vai ser agendada com
            você{view.installPreference ? ` (você pediu: ${view.installPreference})` : ''}.
          </p>
        </section>
      ) : (
        <>
          <section className={`${styles.card} ${stepDone(inspectionSent)}`}>
            <h2 className={local.step}>
              <span className={local.number}>1</span> Vistoria do veículo
            </h2>
            {inspectionSent ? (
              <p className={styles.muted}>Vistoria enviada. {view.organizationName} vai conferir as fotos.</p>
            ) : view.inspectionStatus === 'canceled' ? (
              <p className={styles.muted}>A vistoria foi cancelada. Fale com a associação.</p>
            ) : (
              <>
                <p className={styles.text}>
                  {view.inspectionNote
                    ? `A associação pediu para refazer: ${view.inspectionNote}`
                    : 'Perto do veículo e com boa luz, tire as fotos pelo celular. Leva uns 10 minutos.'}
                </p>
                {view.inspectionExpired ? (
                  <p className={base.error}>O prazo da vistoria acabou: fale com a associação para renovar.</p>
                ) : (
                  <a className={`${base.primary} ${local.action}`} href={view.inspectionLink}>
                    Fazer a vistoria
                  </a>
                )}
              </>
            )}
          </section>

          <section className={`${styles.card} ${stepDone(Boolean(view.acceptedAt))}`}>
            <h2 className={local.step}>
              <span className={local.number}>2</span> Termo do associado e instalação
            </h2>
            {view.acceptedAt ? (
              <p className={styles.muted}>
                Termo aceito em {formatDateTime(view.acceptedAt)}.
                {view.installPreference ? ` Preferência para a instalação: ${view.installPreference}.` : ''}
              </p>
            ) : !inspectionSent ? (
              <p className={styles.muted}>Depois da vistoria.</p>
            ) : (
              <>
                {term && (
                  <>
                    <button type="button" className={`${base.secondary} ${local.action}`} onClick={() => setShowTerm((v) => !v)}>
                      {showTerm ? 'Fechar o termo' : `Ler o termo do associado (versão ${term.version})`}
                    </button>
                    {showTerm && (
                      <div className={page.term}>
                        <ContractText contract={term} />
                      </div>
                    )}
                  </>
                )}
                <textarea
                  className={local.textarea}
                  maxLength={300}
                  value={preference}
                  aria-label="Preferência para a instalação"
                  placeholder="Melhor dia e horário para a instalação (opcional): sábado de manhã, depois das 18h…"
                  onChange={(e) => setPreference(e.target.value)}
                />
                <label className={page.accept}>
                  <input type="checkbox" checked={accept} onChange={(e) => setAccept(e.target.checked)} />
                  <span>Li e aceito o termo do associado: o uso do app e o rastreador em comodato.</span>
                </label>
                {error && <p className={base.error}>{error}</p>}
                <button
                  type="button"
                  className={`${base.primary} ${local.action}`}
                  disabled={!accept || !term || sending}
                  onClick={() => void acceptTerm()}
                >
                  {sending ? 'Enviando…' : 'Enviar a adesão'}
                </button>
              </>
            )}
          </section>

          <section className={`${styles.card} ${stepDone(view.status === 'submitted')}`}>
            <h2 className={local.step}>
              <span className={local.number}>3</span> Análise da associação
            </h2>
            <p className={styles.muted}>
              {view.status === 'submitted'
                ? `Tudo enviado. ${view.organizationName} vai conferir e você recebe o acesso ao app por e-mail quando for aprovada.`
                : 'Depois da vistoria e do termo.'}
            </p>
          </section>
        </>
      )}
    </Shell>
  );
}
