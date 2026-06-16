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
	let savingVisibility = $state(false);

	function checkMobile() {
		isMobile = window.innerWidth < 768;
	}

	function startStream(camera: string) {
		streamErrors[camera] = null;

		try {
			const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
			const wsUrl = `${protocol}//${window.location.host}/api/stream/mse?camera=${camera}`;

			const ws = new WebSocket(wsUrl);
			websockets[camera] = ws;
			ws.binaryType = 'arraybuffer';

			const video = videoElements[camera];
			if (!video) {
				streamErrors[camera] = 'Video element not ready';
				return;
			}

			const mediaSource = new MediaSource();
			video.src = URL.createObjectURL(mediaSource);

			let sourceBuffer: SourceBuffer | null = null;
			let bufferQueue: ArrayBuffer[] = [];

			cleanups[camera] = () => {
				ws.close();
				if (video.src) {
					URL.revokeObjectURL(video.src);
					video.src = '';
				}
			};

			function initSourceBuffer(mimeType: string) {
				if (!MediaSource.isTypeSupported(mimeType)) {
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

	function stopStream(camera: string) {
		if (cleanups[camera]) {
			cleanups[camera]();
			delete cleanups[camera];
		}
		if (websockets[camera]) {
			websockets[camera]!.close();
			delete websockets[camera];
		}
		if (videoElements[camera]) {
			videoElements[camera].src = '';
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
			if (!websockets[cam]) startStream(cam);
		}
		for (const cam of Object.keys(websockets)) {
			if (!current.includes(cam)) stopStream(cam);
		}
	});

	$effect(() => {
		checkMobile();
		window.addEventListener('resize', checkMobile);
		return () => window.removeEventListener('resize', checkMobile);
	});

	onDestroy(() => {
		for (const cam of Object.keys(websockets)) {
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
				<div class={isMobile && i !== active ? 'hidden' : ''}>
					<div class="relative aspect-video overflow-hidden rounded bg-black">
						{#if streamErrors[camera]}
							<div class="flex h-full flex-col items-center justify-center gap-2">
								<p class="text-sm text-red-400">{camera}: {streamErrors[camera]}</p>
								<button
									onclick={() => retryStream(camera)}
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
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
