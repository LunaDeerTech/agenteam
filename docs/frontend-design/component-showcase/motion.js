/* Prototype motion helpers: interruptible, local, and reduced-motion aware. */
window.previewMotion = (() => {
  const enabled = () => document.documentElement.dataset.style === 'linear' && !matchMedia('(prefers-reduced-motion: reduce)').matches;
  const active = new WeakMap();
  const duration = (token, fallback) => parseFloat(getComputedStyle(document.documentElement).getPropertyValue(token)) || fallback;
  function expand(element, open, details = false) {
    const previous = active.get(element);
    const from = element.getBoundingClientRect().height;
    previous?.cancel();
    if (!enabled()) { if (details) element.open = open; else element.hidden = !open; element.style.overflow = ''; return; }
    if (details) element.open = true; else element.hidden = false;
    const to = open ? element.getBoundingClientRect().height : details ? element.querySelector('summary').getBoundingClientRect().height + 2 : 0;
    element.style.overflow = 'hidden';
    const animation = element.animate([{height: from + 'px', opacity: open ? .65 : 1}, {height: to + 'px', opacity: open ? 1 : details ? 1 : 0}], {duration: duration('--motion-panel',200), easing: 'cubic-bezier(.2,.8,.2,1)'});
    active.set(element, animation);
    animation.finished.then(() => { if (active.get(element) !== animation) return; active.delete(element); element.style.overflow = ''; if (details) element.open = open; else element.hidden = !open; }).catch(() => {});
  }
  function leave(element, drawer = false) {
    if (!element) return;
    if (!enabled()) { element.remove(); return; }
    // A noninteractive copy lets focus restore immediately while the visual exits.
    const copy = element.cloneNode(true);copy.inert = true;copy.setAttribute('aria-hidden','true');copy.dataset.exiting = 'true';
    copy.removeAttribute('id');copy.querySelectorAll('[id]').forEach(el => el.removeAttribute('id'));
    document.body.append(copy);element.remove();
    const animation=copy.animate([{opacity:1, transform:'translate(0,0)'},{opacity:0,transform:drawer?'translateX(12px)':'translateY(-4px)'}],{duration:duration('--motion-exit',120),easing:'cubic-bezier(.4,0,1,1)'});
    animation.finished.then(()=>copy.remove()).catch(()=>copy.remove());
  }
  return {expand, leave};
})();
