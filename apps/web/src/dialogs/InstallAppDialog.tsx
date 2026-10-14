import { useT } from '../app/i18n';
import { Dialog } from '../components/common';
import { DownloadButtons } from '../screens/Misc';

/** Install-app alert (R-CLOUD-2). */
export default function InstallAppDialog({ onClose }: { onClose: () => void }) {
  const t = useT();
  return (
    <Dialog role="alertdialog" onClose={onClose} hideClose width={520} labelledBy="install-app-t">
      <h2 id="install-app-t" style={{ margin: 0, fontSize: 22, lineHeight: 1.3, fontWeight: 700 }}>
        {t('add.installTitle')}
      </h2>
      <div style={{ marginTop: 18 }}>
        <DownloadButtons compact />
      </div>
      <button type="button" className="btn btn-secondary btn-block" style={{ marginTop: 14 }} onClick={onClose} data-autofocus>
        {t('common.ok')}
      </button>
    </Dialog>
  );
}
