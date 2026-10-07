import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { authApi, meApi } from '@/api/resources';
import { AddressFields, EMPTY_ADDRESS, isAddressComplete } from '@/components/address/AddressFields';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { TextField } from '@/components/ui/Field';
import { useToast } from '@/components/ui/Toast';
import { formatPhoneInput, isPhoneComplete } from '@/services/format';
import { formatTaxId, taxIdDigits, validTaxId } from '@/services/taxid';
import { useAuth } from '@/stores/AuthContext';
import type { DeliveryAddress, User } from '@/types';

import { BackIcon } from '../icons';
import styles from './Screen.module.css';

interface ProfileForm {
  name: string;
  phone: string;
  document: string;
}

const formOf = (u: User | null): ProfileForm => ({
  name: u?.name ?? '',
  phone: formatPhoneInput(u?.phone ?? ''),
  document: formatTaxId(u?.document ?? ''),
});

const sameAddress = (a: DeliveryAddress, b: DeliveryAddress) =>
  (Object.keys(EMPTY_ADDRESS) as (keyof DeliveryAddress)[]).every((k) => (a[k] ?? '') === (b[k] ?? ''));

/**
 * Meus dados (em Conta): o cliente atualiza o nome, o telefone, o CPF — que
 * é obrigatório, para as notas fiscais — e o endereço de entrega. O e-mail
 * é o login e só aparece.
 */
export function ProfileScreen() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const { user, updateUser } = useAuth();

  // Os dados do servidor ao abrir (o celular pode ter uma versão antiga).
  const me = useQuery({ queryKey: ['me', 'profile'], queryFn: authApi.me });
  const account = useQuery({ queryKey: ['me', 'account'], queryFn: meApi.account });

  const [form, setForm] = useState<ProfileForm>(() => formOf(user));
  const [saved, setSaved] = useState<ProfileForm>(() => formOf(user));
  const [touched, setTouched] = useState(false);
  useEffect(() => {
    if (!me.data) return;
    const fresh = formOf(me.data);
    setSaved(fresh);
    // Só troca o que está na tela se a pessoa ainda não mexeu.
    setForm((current) => (JSON.stringify(current) === JSON.stringify(saved) ? fresh : current));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me.data]);

  const nameError = touched && !form.name.trim() ? 'Informe o nome.' : '';
  const phoneError = touched && form.phone && !isPhoneComplete(form.phone) ? 'Informe o DDD e o número.' : '';
  const documentError =
    touched && !validTaxId(form.document) ? 'CPF inválido: confira os números (para empresa, informe o CNPJ).' : '';
  const dirty = JSON.stringify(form) !== JSON.stringify(saved);

  const save = useMutation({
    mutationFn: () => meApi.updateProfile({ name: form.name, phone: form.phone, document: taxIdDigits(form.document) }),
    onSuccess: (next) => {
      updateUser(next);
      queryClient.setQueryData(['me', 'profile'], next);
      const fresh = formOf(next);
      setForm(fresh);
      setSaved(fresh);
      setTouched(false);
      notify({ tone: 'success', title: 'Dados atualizados' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para salvar', description: err.message }),
  });

  const submit = () => {
    setTouched(true);
    if (!form.name.trim() || (form.phone && !isPhoneComplete(form.phone)) || !validTaxId(form.document)) return;
    save.mutate();
  };

  return (
    <div className={styles.screen}>
      <div className={styles.navbar}>
        <button type="button" className={styles.back} onClick={() => navigate('/conta')} aria-label="Voltar">
          <BackIcon />
        </button>
        <div className={styles.navTitle}>
          <h1>Meus dados</h1>
        </div>
      </div>

      <section className={styles.section} aria-labelledby="dados-titulo">
        <h2 id="dados-titulo" className={styles.sectionTitle}>
          Seus dados
        </h2>
        <form
          className={styles.form}
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
        >
          <TextField
            label="Nome"
            autoComplete="name"
            value={form.name}
            error={nameError || undefined}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
          <TextField label="E-mail" value={user?.email ?? ''} readOnly disabled hint="É o seu login. Para trocar, fale com a gente." />
          <TextField
            label="Celular (WhatsApp)"
            inputMode="tel"
            autoComplete="tel"
            placeholder="(11) 99999-9999"
            value={form.phone}
            error={phoneError || undefined}
            onChange={(e) => setForm({ ...form, phone: formatPhoneInput(e.target.value, form.phone) })}
          />
          <TextField
            label="CPF"
            inputMode="numeric"
            autoComplete="off"
            placeholder="000.000.000-00"
            value={form.document}
            error={documentError || undefined}
            hint="Obrigatório: é com ele que emitimos as notas fiscais (a NF-e do rastreador e a NFS-e das mensalidades). Empresa? Informe o CNPJ."
            onChange={(e) => setForm({ ...form, document: formatTaxId(e.target.value) })}
          />
          <Button type="submit" variant="primary" block loading={save.isPending} disabled={!dirty}>
            Salvar dados
          </Button>
        </form>
      </section>

      <section className={styles.section} aria-labelledby="endereco-titulo">
        <h2 id="endereco-titulo" className={styles.sectionTitle}>
          Endereço de entrega
        </h2>
        <p className={styles.muted}>Para onde enviamos os rastreadores. Digite o CEP: a rua, o bairro e a cidade vêm sozinhos.</p>
        {/* Só com o endereço salvo em mãos: os campos já nascem com ele (sem buscar o CEP de novo). */}
        {account.data ? (
          <AddressSection saved={account.data.deliveryAddress} />
        ) : (
          <Spinner label="Carregando o endereço" />
        )}
      </section>
    </div>
  );
}

/** O endereço de entrega, com o botão de salvar próprio. */
function AddressSection({ saved }: { saved: DeliveryAddress | null }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [address, setAddress] = useState<DeliveryAddress>(saved ?? EMPTY_ADDRESS);
  const dirty = !sameAddress(address, saved ?? EMPTY_ADDRESS);

  const save = useMutation({
    mutationFn: () => meApi.saveAddress(address),
    onSuccess: (next) => {
      setAddress(next);
      void queryClient.invalidateQueries({ queryKey: ['me', 'account'] });
      notify({ tone: 'success', title: 'Endereço atualizado' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para salvar o endereço', description: err.message }),
  });

  return (
    <>
      <AddressFields value={address} onChange={setAddress} />
      <Button
        variant="secondary"
        block
        loading={save.isPending}
        disabled={!dirty || !isAddressComplete(address)}
        onClick={() => save.mutate()}
      >
        Salvar endereço
      </Button>
    </>
  );
}
