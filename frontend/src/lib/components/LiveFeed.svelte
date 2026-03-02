<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import CameraToggle from './CameraToggle.svelte';

	const cameras = ['gate', 'gate-rear'];
	let activeCamera = $state(0);
	let videoElements: HTMLVideoElement[] = $state([]);
	let streamErrors: (string | null)[] = $state([null, null]);
	let websockets: (WebSocket | null)[] = [null, null];
	let mediaSourceCleanups: (() => void)[] = [];
	let isMobile = $state(false);

	function checkMobile() {
		isMobile = window.innerWidth < 768;
	}

	async function startStream(index: number) {
		const camera = cameras[index];
		streamErrors[index] = null;

		try {
			const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
			const wsUrl = `${protocol}//${window.location.host}/api/stream/mse?camera=${camera}`;

			const ws = new WebSocket(wsUrl);
			websockets[index] = ws;

			ws.binaryType = 'arraybuffer';

			const mediaSource = new MediaSource();
			const video = videoElements[index];
			if (!video) {
				streamErrors[index] = 'Video element not ready';
				return;
			}

			video.src = URL.createObjectURL(mediaSource);

			let sourceBuffer: SourceBuffer | null = null;
			let bufferQueue: ArrayBuffer[] = [];
			let cleanup = () => {
				ws.close();
				if (video.src) {
					URL.revokeObjectURL(video.src);
					video.src = '';
				}
			};
			mediaSourceCleanups[index] = cleanup;

			mediaSource.addEventListener('sourceopen', () => {
				// The first text message from go2rtc contains the codec info
				// in the format: {"type":"mse","value":"codec1,codec2,..."}
			});

			ws.onmessage = (event) => {
				if (typeof event.data === 'string') {
					// JSON control message from go2rtc
					try {
						const msg = JSON.parse(event.data);
						if (msg.type === 'mse') {
							// msg.value contains the codecs string
							const codecs = msg.value;
							const mimeType = `video/mp4; codecs="${codecs}"`;

							if (!MediaSource.isTypeSupported(mimeType)) {
								streamErrors[index] = `Unsupported codec: ${codecs}`;
								ws.close();
								return;
							}

							sourceBuffer = mediaSource.addSourceBuffer(mimeType);
							sourceBuffer.mode = 'segments';

							sourceBuffer.addEventListener('updateend', () => {
								if (bufferQueue.length > 0 && sourceBuffer && !sourceBuffer.updating) {
									sourceBuffer.appendBuffer(bufferQueue.shift()!);
								}

								// Keep buffer from growing too large - trim to last 30s
								if (sourceBuffer && !sourceBuffer.updating && video.buffered.length > 0) {
									const end = video.buffered.end(video.buffered.length - 1);
									const start = video.buffered.start(0);
									if (end - start > 60) {
										sourceBuffer.remove(start, end - 30);
									}
								}
							});

							// Flush any data that arrived before sourceBuffer was ready
							if (bufferQueue.length > 0 && !sourceBuffer.updating) {
								sourceBuffer.appendBuffer(bufferQueue.shift()!);
							}
						}
					} catch {
						// Ignore unparseable messages
					}
				} else {
					// Binary data - MSE media segment
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
				streamErrors[index] = 'Connection error';
			};

			ws.onclose = (event) => {
				if (!event.wasClean && !streamErrors[index]) {
					streamErrors[index] = 'Stream disconnected';
				}
			};
		} catch (err) {
			streamErrors[index] = err instanceof Error ? err.message : 'Stream unavailable';
		}
	}

	function stopStream(index: number) {
		if (mediaSourceCleanups[index]) {
			mediaSourceCleanups[index]();
			mediaSourceCleanups[index] = undefined!;
		}
		if (websockets[index]) {
			websockets[index]!.close();
			websockets[index] = null;
		}
		if (videoElements[index]) {
			videoElements[index].src = '';
		}
	}

	function retryStream(index: number) {
		stopStream(index);
		startStream(index);
	}

	onMount(() => {
		checkMobile();
		window.addEventListener('resize', checkMobile);

		for (let i = 0; i < cameras.length; i++) {
			startStream(i);
		}
	});

	onDestroy(() => {
		if (typeof window !== 'undefined') {
			window.removeEventListener('resize', checkMobile);
		}
		for (let i = 0; i < cameras.length; i++) {
			stopStream(i);
		}
	});

	function handleCameraChange(index: number) {
		activeCamera = index;
	}
</script>

<div>
	{#if isMobile}
		<CameraToggle {cameras} active={activeCamera} onChange={handleCameraChange} />
	{/if}
	<div class={isMobile ? '' : 'grid grid-cols-2 gap-2'}>
		{#each cameras as camera, i}
			<div class={isMobile && i !== activeCamera ? 'hidden' : ''}>
				<div class="relative aspect-video overflow-hidden rounded bg-black">
					{#if streamErrors[i]}
						<div class="flex h-full flex-col items-center justify-center gap-2">
							<p class="text-sm text-red-400">{camera}: {streamErrors[i]}</p>
							<button
								onclick={() => retryStream(i)}
								class="rounded bg-gray-700 px-3 py-1 text-xs text-gray-300 hover:bg-gray-600"
							>
								Retry
							</button>
						</div>
					{/if}
					<!-- svelte-ignore a11y_media_has_caption -->
					<video
						bind:this={videoElements[i]}
						autoplay
						playsinline
						muted
						class="h-full w-full object-contain"
						class:hidden={!!streamErrors[i]}
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
</div>
