import { useEffect, useState } from 'react';

import { ApiError } from '@/api/client';
import { publicApi } from '@/api/resources';
import { clearPartnerSession, needsPartnerPassword } from '@/services/partnerSession';
import type { PartnerReport } from '@/types';

/**
 * Os números da página do afiliado, pelo link secreto. Com a senha que a
 * central pôs (e sem a sessão, ou com uma que caiu), locked: a página pede a
 * senha e, depois da entrada, unlock carrega de novo.
 */
export function usePartnerReport(token: string) {
  const [report, setReport] = useState<PartnerReport | null>(null);
  const [error, setError] = useState('');
  const [locked, setLocked] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    publicApi
      .partner(token)
      .then((r) => {
        if (!alive) return;
        setLocked(false);
        setReport(r);
      })
      .catch((err) => {
        if (!alive) return;
        if (needsPartnerPassword(err)) {
          clearPartnerSession(token);
          setLocked(true);
          return;
        }
        setError(
          err instanceof ApiError && err.status === 404
            ? 'Este link não vale mais. Peça o link novo à Farbo.'
            : 'Não deu para carregar agora. Tente de novo em instantes.',
        );
      });
    return () => {
      alive = false;
    };
  }, [token, attempt]);

  return { report, setReport, error, locked, unlock: () => setAttempt((n) => n + 1) };
}
