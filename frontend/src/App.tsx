import { BrowserRouter, Routes, Route, Link, useLocation } from 'react-router-dom';
import { ChefHat, LayoutDashboard, Settings, ScrollText, MonitorPlay } from 'lucide-react';
import Admin from './admin/Admin';
import KDS from './kds/KDS';
import POS from './pos/POS';

function Sidebar() {
  const location = useLocation();

  return (
    <div className="sidebar">
      <div className="sidebar-header">
        <ChefHat size={28} color="#3b82f6" />
        <span>Taste Of Village</span>
      </div>
      <div className="nav-links">
        <Link to="/" className={`nav-link ${location.pathname === '/' ? 'active' : ''}`}>
          <LayoutDashboard size={20} />
          <span>Admin Menu</span>
        </Link>
        <Link to="/pos/taste-of-village" className={`nav-link ${location.pathname.startsWith('/pos') ? 'active' : ''}`}>
          <MonitorPlay size={20} />
          <span>Point of Sale</span>
        </Link>
        <Link to="/kds/taste-of-village" className={`nav-link ${location.pathname.startsWith('/kds') ? 'active' : ''}`}>
          <ScrollText size={20} />
          <span>KDS Station</span>
        </Link>
        <Link to="/settings" className="nav-link">
          <Settings size={20} />
          <span>Settings</span>
        </Link>
      </div>
    </div>
  );
}

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/kds/:storeId" element={<KDS />} />
        <Route
          path="*"
          element={
            <div className="app-container">
              <Sidebar />
              <div className="main-content">
                <Routes>
                  <Route path="/" element={<Admin />} />
                  <Route path="/pos/:storeId" element={<POS />} />
                  <Route path="/settings" element={<div><h1>Settings</h1></div>} />
                </Routes>
              </div>
            </div>
          }
        />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
