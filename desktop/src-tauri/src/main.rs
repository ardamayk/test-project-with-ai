fn main() {
    if let Err(error) = earthly_audio_desktop::run() {
        eprintln!("Earthly Audio Desktop failed: {error}");
        std::process::exit(1);
    }
}
