package release

import (
	"bytes"
	"debug/elf"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"testing"
)

func fixtureELF(machine elf.Machine, class elf.Class, data elf.Data) []byte {
	raw := make([]byte, 64)
	copy(raw, elf.ELFMAG)
	raw[4], raw[5], raw[6] = byte(class), byte(data), byte(elf.EV_CURRENT)
	var order binary.ByteOrder = binary.LittleEndian
	if data == elf.ELFDATA2MSB {
		order = binary.BigEndian
	}
	order.PutUint16(raw[16:], uint16(elf.ET_EXEC))
	order.PutUint16(raw[18:], uint16(machine))
	order.PutUint32(raw[20:], uint32(elf.EV_CURRENT))
	if class == elf.ELFCLASS32 {
		order.PutUint16(raw[40:], 52)
		order.PutUint16(raw[46:], 40)
	} else {
		order.PutUint16(raw[52:], 64)
		order.PutUint16(raw[58:], 64)
	}
	return raw
}

func fixturePE(t *testing.T, machine uint16, wide bool) []byte {
	t.Helper()
	var optional any = pe.OptionalHeader32{Magic: 0x10b, NumberOfRvaAndSizes: 16}
	if wide {
		optional = pe.OptionalHeader64{Magic: 0x20b, NumberOfRvaAndSizes: 16}
	}
	raw := make([]byte, 64)
	copy(raw, "MZ")
	binary.LittleEndian.PutUint32(raw[60:], 64)
	buffer := bytes.NewBuffer(raw)
	buffer.WriteString("PE\x00\x00")
	header := pe.FileHeader{Machine: machine, SizeOfOptionalHeader: uint16(binary.Size(optional)), Characteristics: pe.IMAGE_FILE_EXECUTABLE_IMAGE}
	for _, part := range []any{header, optional} {
		if err := binary.Write(buffer, binary.LittleEndian, part); err != nil {
			t.Fatal(err)
		}
	}
	return buffer.Bytes()
}

func TestExecutableTargetUsesContainerMachineClassAndEndianness(t *testing.T) {
	for _, target := range []struct {
		machine elf.Machine
		class   elf.Class
		data    elf.Data
		arch    string
	}{
		{elf.EM_386, elf.ELFCLASS32, elf.ELFDATA2LSB, "386"},
		{elf.EM_X86_64, elf.ELFCLASS64, elf.ELFDATA2LSB, "amd64"},
		{elf.EM_ARM, elf.ELFCLASS32, elf.ELFDATA2LSB, "arm"},
		{elf.EM_AARCH64, elf.ELFCLASS64, elf.ELFDATA2LSB, "arm64"},
		{elf.EM_MIPS, elf.ELFCLASS32, elf.ELFDATA2MSB, "mips"},
		{elf.EM_MIPS, elf.ELFCLASS32, elf.ELFDATA2LSB, "mipsle"},
		{elf.EM_MIPS, elf.ELFCLASS64, elf.ELFDATA2MSB, "mips64"},
		{elf.EM_MIPS, elf.ELFCLASS64, elf.ELFDATA2LSB, "mips64le"},
		{elf.EM_PPC64, elf.ELFCLASS64, elf.ELFDATA2MSB, "ppc64"},
		{elf.EM_PPC64, elf.ELFCLASS64, elf.ELFDATA2LSB, "ppc64le"},
		{elf.EM_RISCV, elf.ELFCLASS64, elf.ELFDATA2LSB, "riscv64"},
		{elf.EM_S390, elf.ELFCLASS64, elf.ELFDATA2MSB, "s390x"},
		{elf.EM_LOONGARCH, elf.ELFCLASS64, elf.ELFDATA2LSB, "loong64"},
	} {
		t.Run(target.arch, func(t *testing.T) {
			platform, arch, err := executableTarget(bytes.NewReader(fixtureELF(target.machine, target.class, target.data)))
			if err != nil || platform != "linux" || arch != target.arch {
				t.Fatalf("target=%s/%s err=%v", platform, arch, err)
			}
		})
	}
	for _, target := range []struct {
		machine uint16
		wide    bool
		arch    string
	}{
		{pe.IMAGE_FILE_MACHINE_AMD64, true, "amd64"},
		{pe.IMAGE_FILE_MACHINE_ARM64, true, "arm64"},
		{pe.IMAGE_FILE_MACHINE_I386, false, "386"},
	} {
		t.Run("windows/"+target.arch, func(t *testing.T) {
			platform, arch, err := executableTarget(bytes.NewReader(fixturePE(t, target.machine, target.wide)))
			if err != nil || platform != "windows" || arch != target.arch {
				t.Fatalf("target=%s/%s err=%v", platform, arch, err)
			}
		})
	}
	badPE := fixturePE(t, pe.IMAGE_FILE_MACHINE_AMD64, true)
	binary.LittleEndian.PutUint16(badPE[86:], pe.IMAGE_FILE_EXECUTABLE_IMAGE|pe.IMAGE_FILE_DLL)
	for index, invalid := range [][]byte{
		{}, []byte("credential-sentinel"), fixtureELF(elf.EM_X86_64, elf.ELFCLASS32, elf.ELFDATA2LSB),
		fixtureELF(elf.EM_ARM, elf.ELFCLASS32, elf.ELFDATA2MSB), fixtureELF(elf.EM_S390, elf.ELFCLASS64, elf.ELFDATA2LSB),
		fixtureELF(elf.EM_RISCV, elf.ELFCLASS32, elf.ELFDATA2LSB),
		fixturePE(t, pe.IMAGE_FILE_MACHINE_AMD64, false), fixturePE(t, pe.IMAGE_FILE_MACHINE_I386, true), badPE,
	} {
		t.Run(fmt.Sprintf("reject-%d", index), func(t *testing.T) {
			if _, _, err := executableTarget(bytes.NewReader(invalid)); err == nil {
				t.Fatal("unsupported container accepted")
			}
		})
	}
}
