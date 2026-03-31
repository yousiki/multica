import { Instrument_Serif } from "next/font/google";

const instrumentSerif = Instrument_Serif({
  subsets: ["latin"],
  weight: "400",
  variable: "--font-serif",
});

export default function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className={`${instrumentSerif.variable} h-full overflow-x-hidden overflow-y-auto bg-white`}>
      {children}
    </div>
  );
}
