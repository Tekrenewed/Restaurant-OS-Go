import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { ShoppingCart, Send, CreditCard, Trash2, Percent } from 'lucide-react';

interface MenuItem {
    id: string;
    name: string;
    category: string;
    base_price: number;
}

interface CartItem extends MenuItem {
    cart_id: string; // Unique ID for instances of the same item
    quantity: number;
    is_zero_rated: boolean; // Cold takeaway is 0% VAT
    is_takeaway: boolean;
}

export default function POS() {
    const { storeId } = useParams();
    const [catalog, setCatalog] = useState<MenuItem[]>([]);
    const [cart, setCart] = useState<CartItem[]>([]);
    const [applyServiceCharge, setApplyServiceCharge] = useState(false);
    const [activeCategory, setActiveCategory] = useState<string>('All');

    // Simulated Store UUIDs matching backend
    const STORE_UUID = storeId === 'taste-of-village' ? '00000000-0000-0000-0000-000000000000' : '11111111-1111-1111-1111-111111111111';

    // 1. Fetch Menu Catalog from Go Backend
    useEffect(() => {
        fetch('http://localhost:8080/api/v1/catalog')
            .then(res => res.json())
            .then(data => setCatalog(data))
            .catch(() => {
                // Fallback dummy catalog
                setCatalog([
                    { id: '1', name: 'Chicken Tikka Masala', category: 'Curry', base_price: 10.99 },
                    { id: '2', name: 'Peri Peri Half Chicken', category: 'Grill', base_price: 8.99 },
                    { id: '3', name: 'Mango Falooda', category: 'Dessert', base_price: 6.99 },
                    { id: '4', name: 'Lamb Rogan Josh', category: 'Curry', base_price: 12.99 },
                    { id: '5', name: 'Loaded Fries', category: 'Grill', base_price: 4.99 },
                    { id: '6', name: 'Diet Coke (Cold)', category: 'Drinks', base_price: 2.50 },
                ]);
            });
    }, []);

    const categories = ['All', ...Array.from(new Set(catalog.map(i => i.category)))];
    const filteredCatalog = activeCategory === 'All' ? catalog : catalog.filter(i => i.category === activeCategory);

    // 2. Cart Logic
    const addToCart = (item: MenuItem, isTakeaway: boolean = false) => {
        setCart(prev => [
            ...prev,
            {
                ...item,
                cart_id: Math.random().toString(36).substr(2, 9),
                quantity: 1,
                is_takeaway: isTakeaway,
                is_zero_rated: isTakeaway && item.category === 'Drinks', // Simplified rule: Cold drinks takeaway = 0% VAT
            }
        ]);
    };

    const removeFromCart = (cartId: string) => {
        setCart(prev => prev.filter(item => item.cart_id !== cartId));
    };

    // Compute live frontend totals (mirroring Go Backend)
    const calculateTotals = () => {
        let gross = 0;
        let net = 0;

        cart.forEach(item => {
            gross += item.base_price * item.quantity;
            const vatRate = item.is_zero_rated ? 0 : 0.20;
            net += (item.base_price * item.quantity) / (1 + vatRate);
        });

        const vat = gross - net;
        const serviceCharge = applyServiceCharge ? (gross * 0.125) : 0;
        const finalGross = gross + serviceCharge;

        return {
            net: net.toFixed(2),
            vat: vat.toFixed(2),
            sc: serviceCharge.toFixed(2),
            total: finalGross.toFixed(2)
        };
    };

    const totals = calculateTotals();

    // 3. Fire Order to Backend & Printers
    const handleFireOrder = async () => {
        if (cart.length === 0) return;

        const payload = {
            store_id: STORE_UUID,
            source: 'POS',
            apply_service_charge: applyServiceCharge,
            items: cart.map(i => ({
                product_id: i.id, // Using the real Go UUID structure
                name: i.name,
                price_paid: i.base_price,
                is_takeaway: i.is_takeaway,
                is_zero_rated: i.is_zero_rated
            }))
        };

        try {
            const res = await fetch('http://localhost:8080/api/v1/orders', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });

            if (res.ok) {
                // Fire successful sound & clear cart
                new Audio('https://assets.mixkit.co/active_storage/sfx/2013/2013-preview.mp3').play().catch(() => { });
                setCart([]);
                setApplyServiceCharge(false);
            } else {
                alert('Failed to send order to kitchen. Backend returned an error.');
            }
        } catch (err) {
            alert('Could not reach the Go Backend ' + err);
        }
    };

    return (
        <div className="pos-layout">
            {/* Left Pane - Menu Grid */}
            <div className="pos-menu-pane">
                <div className="pos-categories">
                    {categories.map(c => (
                        <button
                            key={c}
                            className={`category-pill ${activeCategory === c ? 'active' : ''}`}
                            onClick={() => setActiveCategory(c)}
                        >
                            {c}
                        </button>
                    ))}
                </div>

                <div className="pos-grid">
                    {filteredCatalog.map(item => (
                        <div key={item.id} className="pos-item-card">
                            <div className="item-name">{item.name}</div>
                            <div className="item-price">£{item.base_price.toFixed(2)}</div>
                            <div style={{ display: 'flex', gap: '8px', marginTop: 'auto', width: '100%' }}>
                                <button
                                    className="takeaway-btn"
                                    style={{ flex: 1, padding: '8px 4px', fontSize: '0.9rem' }}
                                    onClick={(e) => { e.stopPropagation(); addToCart(item, false); }}
                                >
                                    Dine In
                                </button>
                                <button
                                    className="takeaway-btn"
                                    style={{ flex: 1, background: 'var(--accent-hover)', color: 'white', padding: '8px 4px', fontSize: '0.9rem' }}
                                    onClick={(e) => { e.stopPropagation(); addToCart(item, true); }}
                                >
                                    Takeaway
                                </button>
                            </div>
                        </div>
                    ))}
                </div>
            </div>

            {/* Right Pane - Active Cart */}
            <div className="pos-cart-pane glass-panel">
                <div className="cart-header">
                    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                        <ShoppingCart size={24} color="var(--accent-primary)" />
                        <h2>Current Order</h2>
                    </div>
                    <span className="item-count">{cart.length} items</span>
                </div>

                <div className="cart-items">
                    {cart.map(item => (
                        <div key={item.cart_id} className="cart-row">
                            <div className="cart-row-details">
                                <span className="cart-row-name">{item.name}</span>
                                <div style={{ display: 'flex', gap: 8 }}>
                                    {item.is_takeaway && <span className="cart-badge takeaway">TAKEAWAY</span>}
                                    {item.is_zero_rated && <span className="cart-badge zero">0% VAT</span>}
                                </div>
                            </div>
                            <div className="cart-row-price">
                                £{(item.base_price * item.quantity).toFixed(2)}
                                <button className="del-btn" onClick={() => removeFromCart(item.cart_id)}>
                                    <Trash2 size={16} />
                                </button>
                            </div>
                        </div>
                    ))}
                    {cart.length === 0 && (
                        <div className="empty-cart">
                            <ShoppingCart size={48} opacity={0.2} />
                            <p>Tap items to add to order</p>
                        </div>
                    )}
                </div>

                {/* Toggles */}
                <div className="cart-options">
                    <div className="cart-option">
                        <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                            <Percent size={18} />
                            Suggest 12.5% Service
                        </span>
                        <label className="toggle-switch">
                            <input
                                type="checkbox"
                                checked={applyServiceCharge}
                                onChange={(e) => setApplyServiceCharge(e.target.checked)}
                            />
                            <span className="slider"></span>
                        </label>
                    </div>
                </div>

                {/* Financial Breakdown */}
                <div className="cart-totals">
                    <div className="total-row">
                        <span>Net Total</span>
                        <span>£{totals.net}</span>
                    </div>
                    <div className="total-row">
                        <span>VAT (20% / 0%)</span>
                        <span>£{totals.vat}</span>
                    </div>
                    {applyServiceCharge && (
                        <div className="total-row">
                            <span>Service Charge (12.5%)</span>
                            <span>£{totals.sc}</span>
                        </div>
                    )}
                    <div className="grand-total-row">
                        <span>Total to Pay</span>
                        <span>£{totals.total}</span>
                    </div>
                </div>

                {/* Action Buttons */}
                <div className="cart-actions">
                    <button className="action-btn dojo" onClick={() => alert("Connecting to Dojo Terminal...")}>
                        <CreditCard size={20} />
                        Charge Dojo
                    </button>
                    <button className="action-btn fire" onClick={handleFireOrder} disabled={cart.length === 0}>
                        <Send size={20} />
                        Fire to Kitchen
                    </button>
                </div>
            </div>
        </div>
    );
}
