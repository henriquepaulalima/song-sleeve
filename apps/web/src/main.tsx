import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router';
import { Root } from './app/Root';

createRoot(document.getElementById('root')!).render(<Root Router={BrowserRouter} />);
