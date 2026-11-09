import { useState } from 'react';
import type { FormEvent } from 'react';

import type { PinProblem } from '@/services/installPin';

import base from './PreLaunch.module.css';
import styles from './PublicTheft.module.css';
import local from './InstallerPage.module.css';

/**
 * A senha do link do instalador: a central passa 6 números junto com o link
 * (o do representante abre sem). Errada, avisa; errada vezes demais, o link
 * trava até a central mandar outra senha — que entra aqui mesmo.
 */
export function InstallPinGate({ problem, busy, onSubmit }: { problem: PinProblem; busy: boolean; onSubmit: (pin: string) => void }) {
  const [pin, setPin] = useState('');
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (pin.length === 6 && !busy) onSubmit(pin);
  };
  return (
    <form className={styles.card} onSubmit={submit}>
      <h1 className={styles.title}>Digite a senha</h1>
      <p className={styles.text}>A Farbo passou uma senha de 6 números junto com este link. Ela abre o passo a passo da instalação.</p>
      {problem === 'wrong' && <p className={base.error}>Senha errada: confira com a Farbo.</p>}
      {problem === 'locked' && (
        <p className={base.error}>A senha foi digitada errada vezes demais. Peça uma senha nova à Farbo e digite aqui.</p>
      )}
      <label className={base.field}>
        Senha
        <input
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={6}
          value={pin}
          autoFocus
          onChange={(e) => setPin(e.target.value.replace(/\D/g, '').slice(0, 6))}
        />
      </label>
      <button type="submit" className={`${base.primary} ${local.action}`} disabled={pin.length !== 6 || busy}>
        {busy ? 'Abrindo…' : 'Abrir'}
      </button>
    </form>
  );
}
