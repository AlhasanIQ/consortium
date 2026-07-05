import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import './api/axiosSetup';
import App from './App';
import { BackendOfflineBanner } from './components/common/BackendOfflineBanner';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <BackendOfflineBanner />
      <App />
    </BrowserRouter>
  </React.StrictMode>,
);
