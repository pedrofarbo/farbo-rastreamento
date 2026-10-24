import { useEffect, useState } from 'react';

import { fetchBlob } from '@/api/client';

/**
 * Uma imagem que precisa do login (as fotos das ocorrências e da vistoria):
 * baixada com o token e mostrada por um endereço local. Clicar abre grande
 * numa aba nova.
 */
export function AuthImage({ src, alt, className }: { src: string; alt: string; className?: string }) {
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    let local: string | null = null;
    setUrl(null);
    setFailed(false);
    fetchBlob(src)
      .then((blob) => {
        if (!alive) return;
        local = URL.createObjectURL(blob);
        setUrl(local);
      })
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
      if (local) URL.revokeObjectURL(local);
    };
  }, [src]);
  if (failed) return <span className={className}>Não deu para carregar a foto.</span>;
  if (!url) return <span className={className} aria-busy="true" />;
  return (
    <a href={url} target="_blank" rel="noreferrer">
      <img src={url} alt={alt} className={className} />
    </a>
  );
}
