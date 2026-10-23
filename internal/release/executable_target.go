package release

import (
	"debug/elf"
	"debug/pe"
	"errors"
	"io"
)

// executableTarget compares the executable container's machine and bitness
// with the Go build settings. It never loads or executes candidate code.
func executableTarget(reader io.ReaderAt) (string, string, error) {
	invalid := errors.New("panel executable format or machine is unsupported")
	var magic [4]byte
	if _, err := reader.ReadAt(magic[:], 0); err != nil {
		return "", "", invalid
	}
	if string(magic[:]) == elf.ELFMAG {
		file, err := elf.NewFile(reader)
		if err != nil || (file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN) || file.Version != elf.EV_CURRENT {
			return "", "", invalid
		}
		arch := ""
		class, data := file.Class, file.Data
		switch file.Machine {
		case elf.EM_386:
			if class == elf.ELFCLASS32 && data == elf.ELFDATA2LSB {
				arch = "386"
			}
		case elf.EM_X86_64:
			if class == elf.ELFCLASS64 && data == elf.ELFDATA2LSB {
				arch = "amd64"
			}
		case elf.EM_ARM:
			if class == elf.ELFCLASS32 && data == elf.ELFDATA2LSB {
				arch = "arm"
			}
		case elf.EM_AARCH64:
			if class == elf.ELFCLASS64 && data == elf.ELFDATA2LSB {
				arch = "arm64"
			}
		case elf.EM_MIPS:
			if class == elf.ELFCLASS32 {
				arch = "mips"
			} else if class == elf.ELFCLASS64 {
				arch = "mips64"
			}
			if arch != "" && data == elf.ELFDATA2LSB {
				arch += "le"
			}
			if data != elf.ELFDATA2LSB && data != elf.ELFDATA2MSB {
				arch = ""
			}
		case elf.EM_PPC64:
			if class == elf.ELFCLASS64 {
				if data == elf.ELFDATA2MSB {
					arch = "ppc64"
				} else if data == elf.ELFDATA2LSB {
					arch = "ppc64le"
				}
			}
		case elf.EM_RISCV:
			if class == elf.ELFCLASS64 && data == elf.ELFDATA2LSB {
				arch = "riscv64"
			}
		case elf.EM_S390:
			if class == elf.ELFCLASS64 && data == elf.ELFDATA2MSB {
				arch = "s390x"
			}
		case elf.EM_LOONGARCH:
			if class == elf.ELFCLASS64 && data == elf.ELFDATA2LSB {
				arch = "loong64"
			}
		}
		if arch != "" {
			return "linux", arch, nil
		}
	}
	if magic[0] == 'M' && magic[1] == 'Z' {
		file, err := pe.NewFile(reader)
		if err != nil || file.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || file.Characteristics&pe.IMAGE_FILE_DLL != 0 {
			return "", "", invalid
		}
		arch := ""
		switch file.Machine {
		case pe.IMAGE_FILE_MACHINE_AMD64:
			if _, ok := file.OptionalHeader.(*pe.OptionalHeader64); ok {
				arch = "amd64"
			}
		case pe.IMAGE_FILE_MACHINE_I386:
			if _, ok := file.OptionalHeader.(*pe.OptionalHeader32); ok {
				arch = "386"
			}
		case pe.IMAGE_FILE_MACHINE_ARM64:
			if _, ok := file.OptionalHeader.(*pe.OptionalHeader64); ok {
				arch = "arm64"
			}
		}
		if arch != "" {
			return "windows", arch, nil
		}
	}
	return "", "", invalid
}
