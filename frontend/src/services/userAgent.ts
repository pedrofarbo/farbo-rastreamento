/**
 * O aparelho de uma sessão, legível: "iPhone · Safari (iOS 26.6)",
 * "Android · Chrome (Android 14)", "Windows · Edge". No iPhone, o Safari
 * congela o sistema no texto ("iPhone OS 18_7") e diz a versão de verdade
 * em "Version/26.6": é ela que vale.
 */
export function deviceLabel(userAgent: string): string {
  const ua = userAgent.trim();
  if (!ua) return 'Aparelho desconhecido';

  const device = /iPad/.test(ua)
    ? 'iPad'
    : /iPhone/.test(ua)
      ? 'iPhone'
      : /Android/.test(ua)
        ? 'Android'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Macintosh|Mac OS X/.test(ua)
            ? 'Mac'
            : /Linux/.test(ua)
              ? 'Linux'
              : 'Aparelho';

  const browser = /CriOS\//.test(ua)
    ? 'Chrome'
    : /FxiOS\/|Firefox\//.test(ua)
      ? 'Firefox'
      : /EdgiOS\/|EdgA\/|Edg\//.test(ua)
        ? 'Edge'
        : /GSA\//.test(ua)
          ? 'app do Google'
          : /SamsungBrowser\//.test(ua)
            ? 'Samsung Internet'
            : /OPR\/|Opera/.test(ua)
              ? 'Opera'
              : /Chrome\//.test(ua)
                ? 'Chrome'
                : /Safari\//.test(ua)
                  ? 'Safari'
                  : '';

  let system = '';
  if (device === 'iPhone' || device === 'iPad') {
    const safari = browser === 'Safari' ? /Version\/(\d+)\.(\d+)/.exec(ua) : null;
    const os = /OS (\d+)_(\d+)/.exec(ua);
    const version = safari ?? os;
    if (version) system = `iOS ${version[1]}.${version[2]}`;
  } else if (device === 'Android') {
    const android = /Android (\d+)/.exec(ua);
    if (android) system = `Android ${android[1]}`;
  }

  return [device, browser].filter(Boolean).join(' · ') + (system ? ` (${system})` : '');
}
