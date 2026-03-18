self.addEventListener('install', (e) => {
  console.log('Service Worker: Installed');
});

self.addEventListener('fetch', (e) => {
  // Simple pass-through for local Termux files
  e.respondWith(fetch(e.request));
});
