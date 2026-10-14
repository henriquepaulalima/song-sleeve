import { useI18n } from '../app/i18n';
import { Brand } from '../components/common';

/**
 * Draft Terms of Use and Privacy Policy. Registration links to these drafts until
 * the final pages are published on the landing site (R-AUTH-10, §10.3 step 6).
 */
const docs = {
  terms: {
    en: {
      title: 'Terms of Use (draft)',
      body: [
        'Song Sleeve plays and organizes music files that you add. You must own the music you add or have the rights to use it.',
        'Song Sleeve may remove content after a valid takedown request. Send takedown requests to the contact address listed on the Song Sleeve website.',
        'Cloud sync is an early-access feature and may change. Accounts can be deleted at any time from Profile.',
        'This is a draft. The final terms will be published before Song Sleeve is open to everyone.',
      ],
    },
    'pt-BR': {
      title: 'Termos de Uso (rascunho)',
      body: [
        'O Song Sleeve toca e organiza arquivos de música que você adiciona. Você precisa ser dono das músicas que adiciona ou ter o direito de usá-las.',
        'O Song Sleeve pode remover conteúdo após um pedido de remoção válido. Envie pedidos de remoção para o endereço de contato informado no site do Song Sleeve.',
        'A sincronização na nuvem é um recurso de acesso antecipado e pode mudar. Contas podem ser excluídas a qualquer momento no Perfil.',
        'Este é um rascunho. Os termos finais serão publicados antes de o Song Sleeve ser aberto a todos.',
      ],
    },
  },
  privacy: {
    en: {
      title: 'Privacy Policy (draft)',
      body: [
        'We store your account email, your synced songs and their names and artists, your playlists and groups, and records of your devices and sessions.',
        'Songs you keep only on a device never leave that device. The web app stores nothing on your device.',
        'Data is stored outside Brazil, in our hosting provider’s regions. Sessions and change history are kept for 90 days.',
        'You can delete your account from Profile. Under Brazil’s LGPD you can ask about, correct or delete your data through the contact address on the Song Sleeve website.',
        'This is a draft. The final policy will be published before Song Sleeve is open to everyone.',
      ],
    },
    'pt-BR': {
      title: 'Política de Privacidade (rascunho)',
      body: [
        'Guardamos o e-mail da sua conta, suas músicas sincronizadas com nomes e artistas, suas playlists e grupos, e registros dos seus dispositivos e sessões.',
        'Músicas que você mantém só em um dispositivo nunca saem dele. O app web não guarda nada no seu dispositivo.',
        'Os dados ficam fora do Brasil, nas regiões do nosso provedor de hospedagem. Sessões e o histórico de alterações são mantidos por 90 dias.',
        'Você pode excluir sua conta no Perfil. Pela LGPD, você pode pedir informações, correção ou exclusão dos seus dados pelo endereço de contato no site do Song Sleeve.',
        'Este é um rascunho. A política final será publicada antes de o Song Sleeve ser aberto a todos.',
      ],
    },
  },
} as const;

export default function Legal({ doc }: { doc: keyof typeof docs }) {
  const { locale } = useI18n();
  const d = docs[doc][locale];
  return (
    <div className="public">
      <div className="public-top">
        <Brand to="/" />
      </div>
      <main style={{ maxWidth: 680, margin: '40px auto 0' }}>
        <h1 className="page-title" style={{ fontSize: 34 }}>
          {d.title}
        </h1>
        {d.body.map((p) => (
          <p key={p} style={{ fontSize: 16, lineHeight: 1.6, color: 'var(--text-soft)' }}>
            {p}
          </p>
        ))}
      </main>
    </div>
  );
}
