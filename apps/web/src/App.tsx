import "./styles.css";

export default function App() {
  return (
    <div className="appShell">
      <header className="topBar">
        <a className="brand" href="#/products" aria-label="智选购 首页">
          智选购
        </a>
        <nav aria-label="Primary" className="navLinks">
          <a href="#/products">Products</a>
          <a href="#/guide">AI Guide</a>
          <a href="#/cart">Cart</a>
          <a href="#/orders">Orders</a>
        </nav>
        <a className="accountLink" href="#/login">
          Login
        </a>
      </header>

      <main className="mainSurface">
        <section className="statusBanner">
          <div>
            <p className="eyebrow">AI Shopping Guide</p>
            <h1>导购精选，正在更新</h1>
            <p>按预算、用途和库存状态筛选商品，推荐结果只展示后端校验后的 SKU 快照。</p>
          </div>
          <a className="bannerAction" href="#/guide">
            Ask AI
          </a>
        </section>

        <section className="prototypeGrid" aria-label="Product preview">
          {["编程笔记本", "轻薄办公", "旗舰手机", "降噪耳机"].map((title) => (
            <article className="productCard" key={title}>
              <div className="imageSlot" aria-hidden="true" />
              <div className="cardMeta">
                <p className="stockBadge">IN_STOCK</p>
                <h2>{title}</h2>
                <p className="muted">后端价格 / 库存快照</p>
              </div>
            </article>
          ))}
        </section>
      </main>
    </div>
  );
}
