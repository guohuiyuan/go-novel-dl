#[cfg(target_os = "windows")]
fn main() {
    println!("cargo:rerun-if-changed=icon.ico");

    let mut res = winres::WindowsResource::new();
    res.set_icon("icon.ico");
    if let Err(err) = res.compile() {
        panic!("failed to compile windows resources: {err}");
    }
}

#[cfg(not(target_os = "windows"))]
fn main() {}
