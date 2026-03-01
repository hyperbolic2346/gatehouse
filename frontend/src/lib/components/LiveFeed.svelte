<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import CameraToggle from './CameraToggle.svelte';

	const cameras = ['gate', 'gate-rear'];
	let activeCamera = $state(0);
	let videoElements: HTMLVideoElement[] = $state([]);
	let peerConnections: RTCPeerConnection[] = [];
	let isMobile = $state(false);

	function checkMobile() {
		isMobile = window.innerWidth < 768;
	}

	async function startStream(index: number) {
		const camera = cameras[index];
		const pc = new RTCPeerConnection({
			iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
		});
		peerConnections[index] = pc;

		pc.addTransceiver('video', { direction: 'recvonly' });
		pc.addTransceiver('audio', { direction: 'recvonly' });

		pc.ontrack = (event) => {
			if (videoElements[index] && event.streams[0]) {
				videoElements[index].srcObject = event.streams[0];
			}
		};

		const offer = await pc.createOffer();
		await pc.setLocalDescription(offer);

		// Wait for ICE gathering
		await new Promise<void>((resolve) => {
			if (pc.iceGatheringState === 'complete') {
				resolve();
			} else {
				pc.onicegatheringstatechange = () => {
					if (pc.iceGatheringState === 'complete') resolve();
				};
				// Timeout after 2s
				setTimeout(resolve, 2000);
			}
		});

		const answer = await api.webrtcOffer(camera, pc.localDescription!);
		await pc.setRemoteDescription(answer);
	}

	function stopStream(index: number) {
		peerConnections[index]?.close();
		peerConnections[index] = undefined!;
		if (videoElements[index]) {
			videoElements[index].srcObject = null;
		}
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
					<video
						bind:this={videoElements[i]}
						autoplay
						playsinline
						muted
						class="h-full w-full object-contain"
					></video>
					<div class="absolute bottom-2 left-2 rounded bg-black/60 px-2 py-0.5 text-xs text-white">
						{camera}
					</div>
				</div>
			</div>
		{/each}
	</div>
</div>
