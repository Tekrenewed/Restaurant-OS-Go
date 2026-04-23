import { useEffect, useState } from 'react';
import { RefreshCw, Search, Store } from 'lucide-react';

interface MenuItem {
    id: string;
    name: string;
    category: string;
    base_price: number;
}

export default function Admin() {
    const [items, setItems] = useState<MenuItem[]>([]);
    const [loading, setLoading] = useState(true);

    // In a real app we'd fetch from http://localhost:8080/api/v1/catalog
    // Falling back to beautiful dummy data if CORS or server is offline for demo
    useEffect(() => {
        fetch('http://localhost:8080/api/v1/catalog')
            .then(res => res.json())
            .then(data => {
                setItems(data);
                setLoading(false);
            })
            .catch((err) => {
                console.warn('Backend offline, using fallback data:', err);
                setItems([
                    { id: '1', name: 'Chicken Tikka Masala', category: 'Curry', base_price: 10.99 },
                    { id: '2', name: 'Peri Peri Half Chicken', category: 'Grill', base_price: 8.99 },
                    { id: '3', name: 'Mango Falooda', category: 'Dessert', base_price: 6.99 },
                    { id: '4', name: 'Lamb Rogan Josh', category: 'Curry', base_price: 12.99 },
                    { id: '5', name: 'Loaded Fries', category: 'Grill', base_price: 4.99 },
                ]);
                setLoading(false);
            });
    }, []);

    const getBadgeClass = (category: string) => {
        return category.toLowerCase();
    };

    return (
        <div>
            <div className="header-row">
                <div>
                    <h1>Menu Control Center</h1>
                    <p style={{ color: 'var(--text-secondary)', marginTop: 8 }}>
                        Manage pricing across all your branches and delivery apps
                    </p>
                </div>
                <button className="sync-btn">
                    <RefreshCw size={18} />
                    <span>Sync Platforms</span>
                </button>
            </div>

            <div style={{ marginBottom: 24, display: 'flex', gap: 16 }}>
                <div style={{
                    display: 'flex', alignItems: 'center', gap: 12,
                    background: 'rgba(0,0,0,0.2)', padding: '12px 16px',
                    borderRadius: 12, flex: 1, border: '1px solid var(--surface-border)'
                }}>
                    <Search size={20} color="var(--text-secondary)" />
                    <input
                        type="text"
                        placeholder="Search items..."
                        style={{
                            background: 'transparent', border: 'none', color: 'white',
                            outline: 'none', width: '100%', fontSize: '1rem', fontFamily: 'Outfit'
                        }}
                    />
                </div>

                <div style={{
                    display: 'flex', alignItems: 'center', gap: 12,
                    background: 'rgba(0,0,0,0.2)', padding: '12px 16px',
                    borderRadius: 12, border: '1px solid var(--surface-border)'
                }}>
                    <Store size={20} color="var(--text-secondary)" />
                    <select style={{
                        background: 'transparent', border: 'none', color: 'white',
                        outline: 'none', fontSize: '1rem', fontFamily: 'Outfit', cursor: 'pointer'
                    }}>
                        <option>All Branches</option>
                        <option>Taste of Village Hayes</option>
                        <option>Azmoz Dark Kitchen</option>
                    </select>
                </div>
            </div>

            <div className="menu-grid">
                {loading ? (
                    <div>Loading items...</div>
                ) : items.map(item => (
                    <div key={item.id} className="glass-panel menu-card">
                        <div className="card-header">
                            <h3 style={{ fontSize: '1.25rem', fontWeight: 600 }}>{item.name}</h3>
                            <span className={`badge ${getBadgeClass(item.category)}`}>{item.category}</span>
                        </div>

                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 16 }}>
                            <div className="price-row">
                                <span style={{ fontWeight: 500 }}>Dine-In (POS)</span>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
                                    <input type="text" className="price-input" defaultValue={item.base_price.toFixed(2)} />
                                    <label className="toggle-switch">
                                        <input type="checkbox" defaultChecked />
                                        <span className="slider"></span>
                                    </label>
                                </div>
                            </div>

                            <div className="price-row">
                                <span style={{ fontWeight: 500 }}>UberEats</span>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
                                    <input type="text" className="price-input" defaultValue={(item.base_price * 1.3).toFixed(2)} />
                                    <label className="toggle-switch">
                                        <input type="checkbox" defaultChecked />
                                        <span className="slider"></span>
                                    </label>
                                </div>
                            </div>

                            <div className="price-row">
                                <span style={{ fontWeight: 500 }}>Deliveroo</span>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
                                    <input type="text" className="price-input" defaultValue={(item.base_price * 1.3).toFixed(2)} />
                                    <label className="toggle-switch">
                                        <input type="checkbox" defaultChecked />
                                        <span className="slider"></span>
                                    </label>
                                </div>
                            </div>
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}
