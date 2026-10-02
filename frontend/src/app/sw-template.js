/* Service worker do app do cliente (/app/).
 *
 * Gerado no build por vite.config.ts (plugin pwaServiceWorker), que preenche
 * a versão do build e a lista de arquivos do app nas duas constantes abaixo.
 *
 * - App: os arquivos do build ficam guardados; o app abre sem internet.
 * - API: rede primeiro; sem conexão, a última resposta guardada (o cliente
 *   vê os últimos dados dos veículos, faturas e alertas). Nada é guardado de
 *   rotas que mudam algo, e "sair" apaga tudo.
 * - Push: mostra os alertas e abre o veículo no toque.
 */
const VERSION = '__VERSION__';
const PRECACHE = __PRECACHE__;
const SHELL_CACHE = 'farbo-app-' + VERSION;
const API_CACHE = 'farbo-app-api';
const SHELL = '/app/index.html';

// Leituras que vale guardar para ver sem internet (GET, do próprio cliente).
const API_OFFLINE = [/^\/api\/vehicles(\/[^/]+)?$/, /^\/api\/geofences$/, /^\/api\/me\/(account|invoices|subscriptions|fulfillments|alerts|push)$/];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL_CACHE).then((cache) => cache.addAll(PRECACHE)));
  // Não assume sozinho: a página pergunta ao cliente se quer atualizar.
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith('farbo-app-') && k !== SHELL_CACHE && k !== API_CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('message', (event) => {
  const type = event.data && event.data.type;
  if (type === 'SKIP_WAITING') self.skipWaiting();
  // Saiu da conta: os dados guardados eram dela.
  if (type === 'CLEAR_API_CACHE') event.waitUntil(caches.delete(API_CACHE));
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (request.method !== 'GET') return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  // Navegação dentro do app: rede primeiro; sem conexão, o app guardado.
  if (request.mode === 'navigate' && url.pathname.startsWith('/app')) {
    event.respondWith(
      fetch(request).catch(() => caches.match(SHELL, { cacheName: SHELL_CACHE }).then((r) => r || Response.error())),
    );
    return;
  }

  // Arquivos com hash no nome nunca mudam: cache primeiro.
  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/app/icons/')) {
    event.respondWith(
      caches.match(request).then(
        (hit) =>
          hit ||
          fetch(request).then((response) => {
            if (response.ok) {
              const copy = response.clone();
              caches.open(SHELL_CACHE).then((cache) => cache.put(request, copy));
            }
            return response;
          }),
      ),
    );
    return;
  }

  if (API_OFFLINE.some((pattern) => pattern.test(url.pathname))) {
    event.respondWith(
      fetch(request)
        .then((response) => {
          if (response.ok) {
            const copy = response.clone();
            caches.open(API_CACHE).then((cache) => cache.put(url.pathname, copy));
          }
          return response;
        })
        .catch(() => caches.match(url.pathname, { cacheName: API_CACHE }).then((r) => r || Response.error())),
    );
  }
});

self.addEventListener('push', (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = { title: 'Farbo Rastreadores', body: event.data ? event.data.text() : '' };
  }
  const critical = data.severity === 'critical';
  event.waitUntil(
    self.registration.showNotification(data.title || 'Farbo Rastreadores', {
      body: data.body || '',
      icon: '/app/icons/icon-192.png',
      badge: '/app/icons/badge-96.png',
      tag: data.tag || undefined,
      // Alerta novo do mesmo tipo e veículo substitui o anterior, mas avisa de novo.
      renotify: Boolean(data.tag),
      // Segurança (SOS, reboque, bateria desconectada) fica até o cliente ver.
      requireInteraction: critical,
      vibrate: critical ? [300, 120, 300, 120, 300] : [120],
      data: { url: data.url || '/app/' },
      lang: 'pt-BR',
    }),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = new URL((event.notification.data && event.notification.data.url) || '/app/', self.location.origin);
  if (!target.pathname.startsWith('/app')) target.pathname = '/app/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windows) => {
      const open = windows.find((w) => new URL(w.url).pathname.startsWith('/app'));
      if (open) {
        return open.focus().then((w) => (w && 'navigate' in w ? w.navigate(target.href) : w));
      }
      return self.clients.openWindow(target.href);
    }),
  );
});
