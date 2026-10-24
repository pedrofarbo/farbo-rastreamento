/**
 * As fotos que o celular manda (ocorrências e vistoria): reduzidas a
 * MAX_SIDE no lado maior, em JPEG, antes de subir — uma foto de 12 MP vira
 * algumas centenas de KB e sobe rápido no 4G. PDF (o B.O.) e o que o
 * navegador não consegue abrir vão como estão.
 */

export const MAX_SIDE = 1600;
const QUALITY = 0.82;

/** O tamanho reduzido: o lado maior vira max, mantendo a proporção. */
export function fitSize(width: number, height: number, max = MAX_SIDE): { width: number; height: number } {
  const longest = Math.max(width, height);
  if (longest <= max) return { width, height };
  const scale = max / longest;
  return { width: Math.round(width * scale), height: Math.round(height * scale) };
}

export async function compressImage(file: File): Promise<Blob> {
  if (!file.type.startsWith('image/') || typeof createImageBitmap === 'undefined') return file;
  try {
    const bitmap = await createImageBitmap(file);
    const { width, height } = fitSize(bitmap.width, bitmap.height);
    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    canvas.getContext('2d')?.drawImage(bitmap, 0, 0, width, height);
    bitmap.close();
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', QUALITY));
    return blob && blob.size < file.size ? blob : file;
  } catch {
    return file;
  }
}

/** A posição do celular agora (para a vistoria), ou nada se a pessoa não deixar. */
export function currentPosition(timeoutMs = 6000): Promise<{ latitude: number; longitude: number } | null> {
  if (typeof navigator === 'undefined' || !navigator.geolocation) return Promise.resolve(null);
  return new Promise((resolve) => {
    navigator.geolocation.getCurrentPosition(
      (p) => resolve({ latitude: p.coords.latitude, longitude: p.coords.longitude }),
      () => resolve(null),
      { enableHighAccuracy: false, timeout: timeoutMs, maximumAge: 60_000 },
    );
  });
}
