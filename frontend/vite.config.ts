import { defineConfig } from 'vite';
import type { Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import basicSsl from '@vitejs/plugin-basic-ssl';
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import path from 'node:path';
import type { OutputBundle, OutputChunk } from 'rollup';

const landingEntry = path.resolve(__dirname, 'index.html');
const appEntry = path.resolve(__dirname, 'app/index.html');

/**
 * O app do cliente (PWA) mora em /app: rotas como /app/veiculos/123 precisam
 * servir app/index.html (o fallback padrão do Vite mandaria para o painel).
 */
function appRoutes(): Plugin {
  const rewrite = (req: { url?: string }, _res: unknown, next: () => void) => {
    const pathname = (req.url ?? '').split('?')[0];
    if (/^\/app(\/|$)/.test(pathname) && !path.extname(pathname)) req.url = '/app/index.html';
    next();
  };
  return {
    name: 'farbo-app-routes',
    configureServer: (server) => void server.middlewares.use(rewrite),
    configurePreviewServer: (server) => void server.middlewares.use(rewrite),
  };
}

/**
 * Gera /app/sw.js no build: o modelo (src/app/sw-template.js) recebe a versão
 * e a lista exata de arquivos do app — só os do app, não os do painel.
 */
function pwaServiceWorker(): Plugin {
  return {
    name: 'farbo-pwa-service-worker',
    apply: 'build',
    generateBundle(_options, bundle: OutputBundle) {
      const chunks = Object.values(bundle).filter((f): f is OutputChunk => f.type === 'chunk');
      const entry = chunks.find((c) => c.isEntry && c.facadeModuleId === appEntry);
      if (!entry) this.error('entrada do app (app/index.html) não encontrada no build');

      const files = new Set<string>();
      const visit = (chunk: OutputChunk) => {
        if (files.has(chunk.fileName)) return;
        files.add(chunk.fileName);
        chunk.viteMetadata?.importedCss.forEach((css) => files.add(css));
        chunk.viteMetadata?.importedAssets.forEach((asset) => files.add(asset));
        chunk.imports.forEach((name) => {
          const next = bundle[name];
          if (next?.type === 'chunk') visit(next);
        });
      };
      visit(entry!);

      const icons = readdirSync(path.resolve(__dirname, 'public/app/icons')).map((f) => `/app/icons/${f}`);
      const brand = ['/assets/logo-header.png', '/assets/logo-mark.png'];
      const precache = ['/app/index.html', '/app/manifest.webmanifest', ...icons, ...brand, ...[...files].sort().map((f) => `/${f}`)];
      const version = createHash('sha256').update(precache.join('\n')).digest('hex').slice(0, 12);
      const template = readFileSync(path.resolve(__dirname, 'src/app/sw-template.js'), 'utf8');
      // Confere o modelo, não o resultado: o nome de um arquivo do build pode
      // ter "__" no hash (aconteceu), e isso não é marcador esquecido.
      const unknown = template.replaceAll('__VERSION__', '').replaceAll('__PRECACHE__', '').match(/__[A-Z][A-Z_]*__/);
      if (unknown) this.error(`marcador sem valor no modelo do sw.js: ${unknown[0]}`);
      const source = template
        .replaceAll('__VERSION__', version)
        .replaceAll('__PRECACHE__', JSON.stringify(precache, null, 2));
      this.emitFile({ type: 'asset', fileName: 'app/sw.js', source });
    },
  };
}

/**
 * HTTPS de desenvolvimento. Com o certificado de scripts/dev-cert.sh (npm run
 * dev:cert) instalado no celular, o iPhone baixa o ícone e as telas de
 * abertura e aceita service worker. Sem ele, um certificado provisório
 * (basic-ssl): abre depois do aviso, mas o app instalado fica sem ícone.
 */
const certDir = path.resolve(__dirname, '.certs');
const trustedHttps =
  existsSync(path.join(certDir, 'dev-cert.pem')) && existsSync(path.join(certDir, 'dev-key.pem'))
    ? { cert: readFileSync(path.join(certDir, 'dev-cert.pem')), key: readFileSync(path.join(certDir, 'dev-key.pem')) }
    : undefined;

/**
 * A página do site sai do build como landing.html (é o nome que o nginx de
 * produção serve), mas a entrada continua sendo o index.html: assim o build
 * funciona igual na máquina e no Docker, sem precisar copiar arquivo antes.
 */
function landingHtml(): Plugin {
  return {
    name: 'farbo-landing-html',
    apply: 'build',
    enforce: 'post',
    generateBundle(_options, bundle: OutputBundle) {
      const page = bundle['index.html'];
      if (!page || page.type !== 'asset') this.error('index.html não saiu do build');
      delete bundle['index.html'];
      this.emitFile({ type: 'asset', fileName: 'landing.html', source: page.source });
    },
  };
}

export default defineConfig({
  plugins: [react(), appRoutes(), ...(trustedHttps ? [] : [basicSsl()]), pwaServiceWorker(), landingHtml()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  preview: { https: trustedHttps },
  server: {
    https: trustedHttps,
    port: 5173,
    host: true,
    // O Vite recusa hosts desconhecidos (proteção contra DNS rebinding).
    // farbo.localtest.me aponta para 127.0.0.1 e é o endereço cadastrado no
    // Melhor Envios para o retorno do OAuth em desenvolvimento.
    allowedHosts: ['farbo.localtest.me'],
    proxy: {
      // Em desenvolvimento o Vite encaminha para o backend, evitando CORS.
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
        // O backend confere a origem do WebSocket (CORS_ORIGINS, por padrão
        // http://localhost:5173). Com o HTTPS de desenvolvimento e o acesso
        // pelo IP da rede (celular), a origem vira https://<ip>:5173. Quando a
        // página é do próprio Vite, o proxy apresenta a origem cadastrada;
        // origem de outro site passa como veio e continua recusada.
        configure: (proxy) => {
          proxy.on('proxyReqWs', (proxyReq, req) => {
            const origin = req.headers.origin;
            if (!origin || !req.headers.host) return;
            try {
              if (new URL(origin).host === req.headers.host) proxyReq.setHeader('Origin', 'http://localhost:5173');
            } catch {
              /* origem malformada: segue como veio */
            }
          });
        },
      },
    },
  },
  build: {
    outDir: 'dist',
    // Os mapas são gerados para depuração, mas o bundle não aponta para eles
    // e o nginx não os serve: o código-fonte original não fica público.
    sourcemap: 'hidden',
    rollupOptions: {
      // Duas entradas: o painel (/) e o app do cliente (/app/).
      input: { landing: landingEntry, app: appEntry },
    },
  },
});
