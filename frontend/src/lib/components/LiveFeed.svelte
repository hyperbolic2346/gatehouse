<script lang="ts">
	import { onDestroy } from 'svelte';
	import CameraToggle from './CameraToggle.svelte';
	import { user, visibleCameras } from '$lib/stores/auth';
	import { api } from '$lib/api';

	// Cameras the user can currently see (permitted minus hidden), and the full
	// permitted set used by the show/hide manager.
	let cameras = $derived(visibleCameras($user));
	let permitted = $derived($user?.cameras ?? []);

	let activeCamera = $state(0);
	let active = $derived(Math.min(activeCamera, Math.max(0, cameras.length - 1)));

	let videoElements = $state<Record<string, HTMLVideoElement>>({});
	let streamErrors = $state<Record<string, string | null>>({});
	let websockets: Record<string, WebSocket | null> = {};
	let cleanups: Record<string, () => void> = {};

	let isMobile = $state(false);
	let managing = $state(false);

	// The camera shown enlarged over the page, if any. The tile itself is
	// restyled rather than re-created, so the stream keeps playing.
	let expanded = $state<string | null>(null);
	let tiles: Record<string, HTMLDivElement> = {};

	function toggleExpanded(camera: string) {
		expanded = expanded === camera ? null : camera;
	}

	async function goFullscreen(camera: string) {
		const tile = tiles[camera];
		const video = videoElements[camera] as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
		if (document.fullscreenElement) {
			await document.exitFullscreen();
			return;
		}
		try {
			if (tile?.requestFullscreen) {
				await tile.requestFullscreen();
				return;
			}
		} catch {
			// fall through to the video element's own fullscreen
		}
		// iPhone Safari only supports fullscreen on the video itself.
		video?.webkitEnterFullscreen?.();
	}

	$effect(() => {
		if (!expanded) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key === 'Escape' && !document.fullscreenElement) expanded = null;
		};
		window.addEventListener('keydown', onKey);
		return () => window.removeEventListener('keydown', onKey);
	});
	let savingVisibility = $state(false);

	function checkMobile() {
		isMobile = window.innerWidth < 768;
	}

	function startStream(camera: string) {
		streamErrors[camera] = null;

		try {
			const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
			const wsUrl = `${protocol}//${window.location.host}/api/stream/mse?camera=${camera}`;

			const video = videoElements[camera];
			if (!video) {
				streamErrors[camera] = 'Video element not ready';
				return;
			}

			// iPhones (iOS 17.1+) have Apple's ManagedMediaSource instead of
			// MediaSource; older iPhones and iPads have neither but play HLS.
			const MSE: typeof MediaSource | undefined =
				(window as unknown as { ManagedMediaSource?: typeof MediaSource }).ManagedMediaSource ??
				window.MediaSource;
			if (!MSE || forceHLS()) {
				startHLS(camera, video);
				return;
			}
			const managed = MSE !== window.MediaSource;

			const ws = new WebSocket(wsUrl);
			websockets[camera] = ws;
			ws.binaryType = 'arraybuffer';

			const mediaSource = new MSE();
			let fallbackTimer: ReturnType<typeof setTimeout> | undefined;
			if (managed) {
				// ManagedMediaSource only plays with remote playback off, and
				// attaches through srcObject.
				video.disableRemotePlayback = true;
				video.srcObject = mediaSource as unknown as MediaProvider;
				// If it has not shown a picture in 10s, switch this camera
				// to HLS, which every iPhone plays natively.
				fallbackTimer = setTimeout(() => {
					if (video.readyState < HTMLMediaElement.HAVE_CURRENT_DATA && cleanups[camera]) {
						stopStream(camera);
						streamErrors[camera] = null;
						startHLS(camera, video);
					}
				}, 10_000);
			} else {
				video.src = URL.createObjectURL(mediaSource);
			}

			let sourceBuffer: SourceBuffer | null = null;
			let bufferQueue: ArrayBuffer[] = [];

			cleanups[camera] = () => {
				clearTimeout(fallbackTimer);
				ws.close();
				if (managed) {
					video.srcObject = null;
				} else if (video.src) {
					URL.revokeObjectURL(video.src);
					video.src = '';
				}
			};

			function initSourceBuffer(mimeType: string) {
				if (!MSE!.isTypeSupported(mimeType)) {
					streamErrors[camera] = `Unsupported codec: ${mimeType}`;
					ws.close();
					return;
				}

				sourceBuffer = mediaSource.addSourceBuffer(mimeType);
				sourceBuffer.mode = 'segments';

				sourceBuffer.addEventListener('updateend', () => {
					if (bufferQueue.length > 0 && sourceBuffer && !sourceBuffer.updating) {
						sourceBuffer.appendBuffer(bufferQueue.shift()!);
					}

					// Trim buffer to prevent unbounded growth
					if (sourceBuffer && !sourceBuffer.updating && video.buffered.length > 0) {
						const end = video.buffered.end(video.buffered.length - 1);
						const start = video.buffered.start(0);
						if (end - start > 60) {
							sourceBuffer.remove(start, end - 30);
						}
					}
				});

				// Flush queued data
				if (bufferQueue.length > 0 && !sourceBuffer.updating) {
					sourceBuffer.appendBuffer(bufferQueue.shift()!);
				}
			}

			ws.onopen = () => {
				// Tell go2rtc we want MSE streaming
				ws.send(JSON.stringify({ type: 'mse' }));
			};

			ws.onmessage = (event) => {
				if (typeof event.data === 'string') {
					try {
						const msg = JSON.parse(event.data);
						if (msg.type === 'mse') {
							// Server responds with MIME type like:
							// "video/mp4; codecs=\"avc1.64001F,mp4a.40.2\""
							const mimeType = msg.value;
							if (mediaSource.readyState === 'open') {
								initSourceBuffer(mimeType);
							} else {
								mediaSource.addEventListener('sourceopen', () => {
									initSourceBuffer(mimeType);
								});
							}
						}
					} catch {
						// Ignore unparseable messages
					}
				} else {
					// Binary MP4 segment data
					const data = event.data as ArrayBuffer;
					if (sourceBuffer && !sourceBuffer.updating) {
						try {
							sourceBuffer.appendBuffer(data);
						} catch {
							bufferQueue.push(data);
						}
					} else {
						bufferQueue.push(data);
					}
				}
			};

			ws.onerror = () => {
				streamErrors[camera] = 'Connection error';
			};

			ws.onclose = (event) => {
				if (!event.wasClean && !streamErrors[camera]) {
					streamErrors[camera] = 'Stream disconnected';
				}
			};
		} catch (err) {
			streamErrors[camera] = err instanceof Error ? err.message : 'Stream unavailable';
		}
	}

	// Troubleshooting switch: ?live=hls (or localStorage.liveVideo = 'hls')
	// forces the HLS path that older iPhones use.
	function forceHLS(): boolean {
		if (new URLSearchParams(window.location.search).get('live') === 'hls') return true;
		try {
			return localStorage.getItem('liveVideo') === 'hls';
		} catch {
			return false;
		}
	}

	// HLS for browsers with no MediaSource at all (older iPhones and iPads),
	// which play it natively in the video element.
	function startHLS(camera: string, video: HTMLVideoElement) {
		if (!video.canPlayType('application/vnd.apple.mpegurl')) {
			streamErrors[camera] = 'Live video is not supported in this browser';
			return;
		}
		const onError = () => {
			streamErrors[camera] = 'Stream disconnected';
		};
		video.addEventListener('error', onError);
		video.src = `/api/stream/hls/stream.m3u8?src=${encodeURIComponent(camera)}`;
		video.play().catch(() => {
			// autoplay may wait for a tap; the video is muted so it usually won't
		});
		cleanups[camera] = () => {
			video.removeEventListener('error', onError);
			video.removeAttribute('src');
			video.load();
		};
	}

	function stopStream(camera: string) {
		if (cleanups[camera]) {
			cleanups[camera]();
			delete cleanups[camera];
		}
		if (websockets[camera]) {
			websockets[camera]!.close();
			delete websockets[camera];
		}
	}

	function retryStream(camera: string) {
		stopStream(camera);
		startStream(camera);
	}

	// Start/stop streams to match the set of visible cameras. Runs whenever the
	// visible set changes (e.g. the user hides or shows a camera).
	$effect(() => {
		const current = cameras;
		for (const cam of current) {
			if (!cleanups[cam]) startStream(cam);
		}
		for (const cam of Object.keys(cleanups)) {
			if (!current.includes(cam)) stopStream(cam);
		}
	});

	$effect(() => {
		checkMobile();
		window.addEventListener('resize', checkMobile);
		return () => window.removeEventListener('resize', checkMobile);
	});

	onDestroy(() => {
		for (const cam of Object.keys(cleanups)) {
			stopStream(cam);
		}
	});

	function handleCameraChange(index: number) {
		activeCamera = index;
	}

	async function toggleVisibility(camera: string) {
		const u = $user;
		if (!u) return;
		const hidden = new Set(u.hidden_cameras ?? []);
		if (hidden.has(camera)) {
			hidden.delete(camera);
		} else {
			hidden.add(camera);
		}
		savingVisibility = true;
		try {
			const updated = await api.setCameraVisibility([...hidden]);
			user.set(updated);
		} catch {
			// Leave state unchanged on failure.
		} finally {
			savingVisibility = false;
		}
	}
</script>

<div>
	<div class="mb-2 flex items-center justify-between">
		{#if isMobile && cameras.length > 1}
			<CameraToggle {cameras} {active} onChange={handleCameraChange} />
		{:else}
			<span></span>
		{/if}
		{#if permitted.length > 1}
			<button
				onclick={() => (managing = !managing)}
				class="rounded px-2 py-1 text-xs text-gray-400 hover:text-gray-200"
				aria-expanded={managing}
			>
				{managing ? 'Done' : 'Cameras'}
			</button>
		{/if}
	</div>

	{#if managing}
		<div class="mb-2 flex flex-wrap gap-2 rounded bg-gray-800 p-2">
			{#each permitted as camera (camera)}
				{@const shown = cameras.includes(camera)}
				<button
					onclick={() => toggleVisibility(camera)}
					disabled={savingVisibility}
					class="rounded px-2 py-1 text-xs {shown
						? 'bg-blue-600 text-white'
						: 'bg-gray-700 text-gray-400'} disabled:opacity-50"
				>
					{shown ? '👁 ' : '🚫 '}{camera}
				</button>
			{/each}
		</div>
	{/if}

	{#if cameras.length === 0}
		<div class="flex aspect-video items-center justify-center rounded bg-black text-sm text-gray-400">
			{#if permitted.length === 0}
				No cameras assigned to your account.
			{:else}
				All cameras hidden — tap “Cameras” to show one.
			{/if}
		</div>
	{:else}
		<div class={isMobile ? '' : cameras.length > 1 ? 'grid grid-cols-2 gap-2' : ''}>
			{#each cameras as camera, i (camera)}
				{@const big = expanded === camera}
				<div class={isMobile && i !== active ? 'hidden' : ''}>
					<div
						bind:this={tiles[camera]}
						role="button"
						tabindex="0"
						aria-label={big ? `Shrink ${camera}` : `Enlarge ${camera}`}
						onclick={() => toggleExpanded(camera)}
						onkeydown={(e) => {
							if (e.key === 'Enter' || e.key === ' ') {
								e.preventDefault();
								toggleExpanded(camera);
							}
						}}
						class="overflow-hidden bg-black {big
							? 'fixed inset-0 z-50 cursor-zoom-out'
							: 'relative aspect-video cursor-zoom-in rounded'}"
					>
						{#if streamErrors[camera]}
							<div class="flex h-full flex-col items-center justify-center gap-2">
								<p class="text-sm text-red-400">{camera}: {streamErrors[camera]}</p>
								<button
									onclick={(e) => {
										e.stopPropagation();
										retryStream(camera);
									}}
									class="rounded bg-gray-700 px-3 py-1 text-xs text-gray-300 hover:bg-gray-600"
								>
									Retry
								</button>
							</div>
						{/if}
						<!-- svelte-ignore a11y_media_has_caption -->
						<video
							bind:this={videoElements[camera]}
							autoplay
							playsinline
							muted
							class="h-full w-full object-contain"
							class:hidden={!!streamErrors[camera]}
						></video>
						<div
							class="absolute bottom-2 left-2 rounded bg-black/60 px-2 py-0.5 text-xs text-white"
						>
							{camera}
						</div>
						<div class="absolute top-2 right-2 flex gap-2">
							<button
								onclick={(e) => {
									e.stopPropagation();
									goFullscreen(camera);
								}}
								class="rounded bg-black/60 px-2 py-1 text-sm text-white hover:bg-black/80"
								aria-label="Full screen"
								title="Full screen"
							>
								⛶
							</button>
							{#if big}
								<button
									onclick={(e) => {
										e.stopPropagation();
										expanded = null;
									}}
									class="rounded bg-black/60 px-3 py-1 text-sm text-white hover:bg-black/80"
								>
									✕ Close
								</button>
							{/if}
						</div>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
